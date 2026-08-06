# Modelo de dados

Decisões que explicam esta forma: [`ARCHITECTURE.md` §4.5](../ARCHITECTURE.md) (modelagem em três níveis), §4.14 (multi-tenancy), §4.31 (identificadores) e §4.32 (payload byte-exato).

```mermaid
erDiagram
    organizations   ||--o{ applications      : "possui"
    applications    ||--o{ endpoints         : "possui"
    applications    ||--o{ events            : "recebe"
    events          ||--o{ deliveries        : "gera uma por endpoint"
    endpoints       ||--o{ deliveries        : "é destino de"
    deliveries      ||--o{ delivery_attempts : "acumula"

    organizations {
        uuid id PK
        text name
        timestamptz created_at
    }

    applications {
        uuid id PK
        uuid org_id FK
        text name
        text api_key_hash "hasheada: só se verifica"
        text plan "default free"
        text backoff_profile "production | demo"
        text locale "default pt-BR"
        timestamptz created_at
    }

    endpoints {
        uuid id PK
        uuid application_id FK
        text url "https obrigatório, faixas privadas bloqueadas"
        bytea secret_encrypted "cifrada: precisa do valor em claro para assinar"
        text status "active | disabled"
        int consecutive_failures "zera em qualquer sucesso"
        timestamptz disabled_at
        timestamptz created_at
    }

    events {
        uuid id PK
        uuid application_id FK
        text event_type
        text payload "bytes crus, byte-exato; NULL = expurgado após 30 dias"
        text idempotency_key "UNIQUE com application_id"
        timestamptz received_at
    }

    deliveries {
        uuid id PK
        uuid event_id FK
        uuid endpoint_id FK
        text status "pending | delivering | succeeded | failed | dead"
        int attempt_count
        timestamptz next_attempt_at
        timestamptz completed_at
        timestamptz created_at
    }

    delivery_attempts {
        uuid id PK
        uuid delivery_id FK
        int attempt_number
        int status_code
        int response_time_ms
        text response_snippet "primeiros 2KB"
        text error
        timestamptz attempted_at
    }
```

## O que a forma está dizendo

**Três níveis, não um.** `event` é o que aconteceu, `delivery` é para quem, `delivery_attempt` é cada POST. Colapsar isso obrigaria a duplicar payload por destino e destruiria a pergunta que o painel precisa responder: "esse evento chegou em 3 dos 4 endpoints?".

**`dead` é estado separado de `failed`.** O replay manual só faz sentido a partir da DLQ, e essa distinção é o que permite a interface oferecer o botão apenas onde ele se aplica.

**Duas credenciais guardadas de formas diferentes.** `api_key_hash` é hasheada porque só se verifica se bate; `secret_encrypted` é cifrada porque o vhook precisa do valor em claro para calcular o HMAC. Não é inconsistência — é a diferença entre verificar e usar.

**`delivery_attempts` é a tabela que cresce mais rápido.** Retenção de 30 dias, e o degrau de particionamento por mês está em §4.27.

**Todo `id` é UUIDv7 gerado na aplicação, não pelo banco.** O ingress precisa do `delivery_id` antes do insert, porque é ele que vai para a fila (§4.6). Na API o mesmo valor aparece como `evt_01HQZX…` — prefixo por recurso mais base32 dos mesmos 128 bits (§4.31). No `psql`, `vhook_id('evt_01HQZX…')` faz o caminho de volta.

**`payload` é `text`, e isso não é descuido.** `jsonb` reordena chaves e re-renderiza na leitura, o que quebraria o HMAC calculado sobre os bytes crus (§4.7). O tipo que "parece certo" é o que estragaria a assinatura (§4.32).

## Índices que existem desde a primeira migration

| Tabela | Índice | Para quê |
|---|---|---|
| `applications` | `UNIQUE (api_key_hash)` | autenticação do ingress |
| `endpoints` | `(application_id)` | listagem por application |
| `events` | `UNIQUE (application_id, idempotency_key)` | idempotência (§4.13) |
| `deliveries` | `(created_at, id)` | paginação keyset (§4.21) |
| `deliveries` | `(status, next_attempt_at)` | varredura do reconciliador (§4.20) |
| `delivery_attempts` | `UNIQUE (delivery_id, attempt_number)` | uma linha por tentativa |

Nascem todos juntos porque criá-los depois, sobre tabela já em uso, exigiria `CREATE INDEX CONCURRENTLY` — e essa é uma complicação que a spec de ingress teria de resolver sem que ela tenha nada a ver com ingress.
