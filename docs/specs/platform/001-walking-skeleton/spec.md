# 001 — Walking skeleton

| | |
|---|---|
| **Status** | em revisão |
| **Release alvo** | `v0.1.0` |
| **Plano** | [`plan.md`](plan.md) |

---

## Problema

Não existe código. Nenhuma spec de feature pode começar sem schema, geração de tipos a partir do contrato, registro de erros e um processo que sobe — e se nenhuma spec fizer isso explicitamente, a primeira feature carrega tudo isso escondido dentro dela, competindo por atenção com a própria feature.

## Escopo

**Entra**

| Peça | Conteúdo |
|---|---|
| `docker-compose.yml` | serviços `postgres`, `rabbitmq` e `api` |
| `Dockerfile` | multi-stage, binário estático, imagem final `gcr.io/distroless/static` |
| `Makefile` | `up` `down` `run` `generate` `test` `test-integration` |
| `migrations/` | as 7 tabelas de [§4.5](../../../ARCHITECTURE.md), via `golang-migrate`, aplicadas no boot da `api` sob advisory lock |
| `internal/store` | pool `pgxpool` e `sqlc.yaml`; sem query própria — o schema já basta para gerar `models.go` |
| `internal/errs` | os tipos de [`ERRORS.md`](../../../ERRORS.md) com nível e status, e as 4 primeiras constantes |
| `i18n/errors.{pt-BR,en,es,fr}.json` | catálogo por locale, `go:embed`, com teste de completude |
| `internal/ids` | UUIDv7 ↔ `prefixo_base32`: `Encode` e `Parse` |
| `internal/obs` | `slog` em JSON, middlewares de correlation id e de recover, handlers de `/healthz`, `/readyz` e `/metrics` |
| `contracts/openapi.yaml` | `/healthz`, `/readyz` e `/metrics` em `paths`; `make generate` com `oapi-codegen`, alvo `chi-server` |
| `cmd/api/main.go` | o único binário desta release; monta o router `chi` e encadeia os middlewares |
| `.env.example` | `DATABASE_URL`, `RABBITMQ_URL`, `VHOOK_HTTP_ADDR` |

**Não entra**

- **Topologia RabbitMQ e a constante de 64 shards** — nada publica nem consome ainda. Nascem em `queue/`, junto com quem as usa; declarar 64 filas que ninguém abre é infraestrutura sem dono.
- **`worker`, `reconciler` e `sink`** — seriam três `main.go` vazios, e um `docker compose ps` que lista processos sem trabalho deixa de ser retrato honesto do sistema.
- **Prometheus e Grafana** — a parte cara deles é o painel de [§4.26](../../../ARCHITECTURE.md), e as séries que valem gráfico (profundidade por shard, DLQ, taxa de entrega) só existem depois que houver fila e worker. Vão para uma spec própria em `platform/`.
- **Autenticação** — os três endpoints desta release são públicos por natureza. `ApplicationApiKey` e `AdminToken` já estão no contrato e nascem com a primeira rota que os exige.
- **Geração de tipos TypeScript** — `apps/dashboard` não existe, e o job de front do CI sai limpo por ausência de `pnpm-workspace.yaml`. Entra com o primeiro app.
- **Configuração do Coolify e CD** — o `docker-compose.yml` já descreve o deploy; a fiação (domínio, variáveis, webhook de build) é a spec de deploy.
- **Seed de dados** — nenhuma rota desta release lê dado de tenant.
- **Código de erro para ID malformado** — nenhuma rota da 001 recebe ID no caminho. `internal/ids` devolve erro sentinela de Go, e o código nasce na spec da primeira rota que aceita um.

## Comportamento observável

### `GET /healthz`

```
→ 200 {"status":"ok"}
```

Liveness pura. **Nunca consulta dependência.** Um blip do Postgres não pode fazer o orquestrador matar um processo saudável — é para isso que existe `/readyz` separado.

### `GET /readyz`

```
→ 200 {"status":"ready","checks":{"postgres":"ok","rabbitmq":"ok"}}
```

```
→ 503 {"error":{"code":"STO-DEP-001",
                "correlation_id":"01HQZX3K7YB2N4M8P6R9T5V0W1",
                "details":[{"field":"postgres","code":"STO-DEP-001"}]}}
```

A checagem tem ordem fixa: `postgres`, depois `rabbitmq`. O `code` do topo é o da **primeira** falha nessa ordem; `details` lista **todas** as que falharam. Cada checagem tem timeout próprio de 2s.

| Situação | Resultado |
|---|---|
| Postgres e Rabbit alcançáveis | 200 com ambos `"ok"` |
| Só Postgres fora | 503, code `STO-DEP-001`, `details` com uma entrada |
| Só Rabbit fora | 503, code `QUE-DEP-001`, `details` com uma entrada |
| Ambos fora | 503, code `STO-DEP-001`, `details` com duas entradas |
| Checagem excede 2s | Tratada como falha, mesmo código do respectivo recurso |

### `GET /metrics`

```
→ 200, content-type text/plain, formato de exposição Prometheus
```

Contém as coletas padrão do `client_golang` (`go_*`, `process_*`) e `vhook_build_info{version,commit}` com valor `1`. Nenhuma métrica leva `application_id` ([§4.26](../../../ARCHITECTURE.md)).

### Correlation id em toda resposta

Toda resposta, de sucesso ou de erro, leva `X-Vhook-Correlation-Id`. O valor é um UUIDv7 gerado pela `api`, renderizado em base32 sem prefixo — o mesmo formato do exemplo já presente no `openapi.yaml`.

| Situação | Resultado |
|---|---|
| Cliente não manda `X-Correlation-Id` | Só o nosso, em header e em toda linha de log da requisição |
| Cliente manda `X-Correlation-Id` válido (≤64 caracteres, `[A-Za-z0-9_-]+`) | O nosso continua sendo o de rastreio; o do cliente vai para o log no campo `client_correlation_id` |
| Cliente manda `X-Correlation-Id` inválido | Descartado silenciosamente, requisição segue normal, log registra `client_correlation_id_dropped: true` |

O valor do cliente **nunca** é reutilizado como o nosso. Se fosse, o identificador de rastreio do sistema passaria a ser controlado por quem chama — repetido por bug ou de propósito, duas requisições distintas viram a mesma linha na investigação.

## Modelo de dados

Migration única criando as 7 tabelas de [§4.5](../../../ARCHITECTURE.md). O modelo já está fechado, então escrever de uma vez evita uma sequência de `ALTER TABLE` sobre tabelas recém-criadas — e evita que a spec de ingress precise criar `deliveries` com `CREATE INDEX CONCURRENTLY` sobre tabela em uso.

### Chave primária

`uuid` em todas as tabelas, **UUIDv7 gerado em Go, sem `DEFAULT` no banco**. O fluxo de ingress grava a linha e publica na fila, então o ID precisa existir antes do insert.

A forma externa é `prefixo_base32` — ver [§4.31](../../../ARCHITECTURE.md), decisão gerada por esta spec.

### Enums

`deliveries.status`, `endpoints.status`, `applications.plan`, `applications.backoff_profile` e `applications.locale` são `text` com `CHECK`, não tipo `ENUM` do Postgres. `ALTER TYPE ... ADD VALUE` tem restrições transacionais que atrapalham migration, e `sqlc` mapeia `text` para `string` sem gerar um tipo intermediário.

### DDL

```sql
CREATE TABLE organizations (
    id          uuid PRIMARY KEY,
    name        text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE applications (
    id               uuid PRIMARY KEY,
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name             text NOT NULL,
    api_key_hash     text NOT NULL,
    plan             text NOT NULL DEFAULT 'free'
                     CHECK (plan IN ('free')),
    backoff_profile  text NOT NULL DEFAULT 'production'
                     CHECK (backoff_profile IN ('production', 'demo')),
    locale           text NOT NULL DEFAULT 'pt-BR'
                     CHECK (locale IN ('pt-BR', 'en', 'es', 'fr')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (api_key_hash)
);

CREATE TABLE endpoints (
    id                    uuid PRIMARY KEY,
    application_id        uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    url                   text  NOT NULL,
    secret_encrypted      bytea NOT NULL,
    status                text  NOT NULL DEFAULT 'active'
                          CHECK (status IN ('active', 'disabled')),
    consecutive_failures  integer     NOT NULL DEFAULT 0,
    disabled_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX endpoints_application_id_idx ON endpoints (application_id);

CREATE TABLE events (
    id               uuid PRIMARY KEY,
    application_id   uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    event_type       text NOT NULL,
    payload          text,
    idempotency_key  text,
    received_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (application_id, idempotency_key)
);

CREATE TABLE deliveries (
    id               uuid PRIMARY KEY,
    event_id         uuid NOT NULL REFERENCES events(id)    ON DELETE CASCADE,
    endpoint_id      uuid NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','delivering','succeeded','failed','dead')),
    attempt_count    integer NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz,
    completed_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX deliveries_keyset_idx     ON deliveries (created_at, id);
CREATE INDEX deliveries_reconciler_idx ON deliveries (status, next_attempt_at);

CREATE TABLE delivery_attempts (
    id                uuid PRIMARY KEY,
    delivery_id       uuid    NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    attempt_number    integer NOT NULL,
    status_code       integer,
    response_time_ms  integer,
    response_snippet  text,
    error             text,
    attempted_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (delivery_id, attempt_number)
);
```

### `events.payload` é `text`, não `jsonb`

[§4.5](../../../ARCHITECTURE.md) dizia `jsonb`. Isso quebraria o invariante de assinatura, e esta spec corrige a tabela lá.

`jsonb` não guarda bytes, guarda uma árvore analisada: reordena chaves, descarta duplicatas e re-renderiza na leitura. O payload que sai do banco não seria byte a byte o que entrou, e o HMAC é sobre `"{timestamp}.{raw_body}"` ([§4.7](../../../ARCHITECTURE.md)) — a verificação do cliente falharia de forma intermitente, que é exatamente o modo de falha que §4.7 diz estar evitando.

`text` preserva os bytes e mantém `payload::jsonb` disponível para consulta ad-hoc no `psql`. O Postgres valida UTF-8 na coluna, o que é desejável — JSON é UTF-8 por definição — mas move a rejeição de bytes malformados para o momento do insert; o ingress valida antes, na spec que o introduzir.

A coluna é **nullable**: `NULL` significa expurgado pela retenção de 30 dias ([§4.27](../../../ARCHITECTURE.md)), e `received_at` diz quando o evento chegou. Não há coluna extra para marcar o expurgo.

Ver [§4.32](../../../ARCHITECTURE.md), decisão gerada por esta spec.

### Função `vhook_id(text) → uuid`

Criada por migration. Decodifica a forma externa de volta para o `uuid` armazenado, para que uma investigação possa escrever direto no `psql`:

```sql
SELECT * FROM events WHERE id = vhook_id('evt_01HQZX3K7YB2N4M8P6R9T5V0W1');
```

- Aceita com prefixo (`evt_01HQ…`) ou sem (`01HQ…`).
- Aceita minúsculas e as ambiguidades do alfabeto Crockford na entrada: `I` e `L` valem `1`, `O` vale `0`.
- Entrada inválida levanta `invalid_text_representation` (SQLSTATE `22P02`), o mesmo comportamento de `'x'::uuid` — um typo aparece em vez de virar `NULL` silencioso numa cláusula `WHERE`.
- Declarada `IMMUTABLE` e `STRICT`.

Ela existe porque a escolha de base32 na borda tem um custo operacional real: o `psql` mostra o UUID cru, e investigar um `evt_01HQ…` relatado exigiria decodificar na mão. O risco de ter o encoding escrito duas vezes é coberto pelo teste de vetores compartilhados descrito abaixo.

## Invariantes tocados

| Invariante | Como continua valendo |
|---|---|
| Migrations rodam no boot da `api` sob advisory lock | `golang-migrate` já toma advisory lock do Postgres internamente; não escrevemos o lock à mão |
| Payload trafega como bytes crus do ingress até o POST | A coluna vira `text`, byte-exata. Era o único ponto do schema onde o invariante podia morrer em silêncio |
| Paginação keyset em `(created_at, id)`, nunca `OFFSET` | O índice nasce agora, antes de `deliveries` ter volume |
| Registro de erros sem texto; catálogo sem comportamento | `internal/errs` tem código, nível e status; `i18n/errors.<locale>.json` tem só texto. Teste de completude liga os dois |
| Nenhuma métrica leva `application_id` | A única métrica própria da release é `vhook_build_info{version,commit}` |
| Ambiente só para segredo e endereço de infraestrutura | `.env.example` tem três variáveis, todas endereço. Nada de comportamento |
| `internal/core` não importa nada | Esta release não cria `internal/core`: não há regra de domínio ainda. `internal/ids` é puro e sem I/O, mas depende de uma biblioteca de UUID — e é exatamente por isso que fica em pacote próprio em vez de dentro de `core` |
| Código gerado nunca é editado à mão | `make generate` seguido de `git diff --exit-code` no CI |

Não toca: ordem de ack, DLQ por publicação explícita, mensagem magra, constante de shards, classificação de falha, timeout de 5s, SSRF, cifra do secret. Todos pertencem a specs que ainda não existem.

## Modos de falha

| Falha | Comportamento esperado | Observável onde |
|---|---|---|
| Postgres fora no boot | A migration falha; o processo sai com código ≠ 0 sem abrir porta | log `error` com `STO-DEP-001` e exit code |
| RabbitMQ fora no boot | A `api` **sobe** — não há migration de fila e nada publica ainda | `/readyz` 503 `QUE-DEP-001`, `/healthz` 200 |
| Postgres cai depois do boot | `/healthz` segue 200; `/readyz` passa a 503 | `STO-DEP-001` no corpo e no log |
| RabbitMQ cai depois do boot | Idem, com `QUE-DEP-001` | `QUE-DEP-001` no corpo e no log |
| Duas instâncias da `api` subindo ao mesmo tempo | O advisory lock serializa: uma aplica as migrations, a outra espera e segue | log das duas, uma reportando 0 migrations aplicadas |
| Migration falha no meio da aplicação | `golang-migrate` marca a versão como `dirty`; **todo boot seguinte recusa** até intervenção manual | exit code ≠ 0 e log com a versão suja |
| Panic dentro de um handler | Middleware de recover devolve 500 `SYS-INT-001` com `correlation_id`; o processo continua servindo | log `error` com stack trace, nunca no corpo da resposta |
| Variável de ambiente obrigatória ausente | Processo sai antes de abrir porta | log `error` com `CFG-VAL-001` nomeando a variável |
| `X-Correlation-Id` do cliente malformado | Descartado; a requisição segue e responde normalmente | `client_correlation_id_dropped: true` no log |
| `/readyz` recebido durante o desligamento | Passa a responder 503 assim que o sinal de término chega, antes de o servidor parar de aceitar conexão | drenagem correta atrás de load balancer |

## Como se prova que funciona

### Unidade — sem container

**`internal/ids`**
- Round-trip `Encode` → `Parse` sobre os vetores fixos de `internal/ids/testdata/vectors.json`.
- Os vetores cobrem: UUID todo zero, UUID todo `f`, um UUIDv7 real, uma entrada minúscula e uma com os caracteres ambíguos de Crockford (`I`, `L`, `O`).
- Prefixo errado é rejeitado: `Parse("evt_", "dlv_01HQ…")` devolve erro.
- Base32 inválido é rejeitado: caractere fora do alfabeto, comprimento ≠ 26.
- Os 48 bits de timestamp do UUIDv7 sobrevivem à ida e volta, e a ordem lexicográfica das strings codificadas acompanha a ordem temporal.

**`internal/errs`**
- Todo código do registro tem entrada nos quatro locales — é o teste que [§4.29](../../../ARCHITECTURE.md) exige.
- Nenhum locale tem entrada órfã, sem código correspondente no registro.
- Nenhum código duplicado; todos casam com `^[A-Z]{3}-[A-Z]{3}-[0-9]{3}$`.
- Nível e status de cada constante batem com o default do seu tipo, salvo quando a constante declara sobrescrita explícita.

**Middleware de correlation**
- Gera e devolve `X-Vhook-Correlation-Id` quando o cliente não manda nada.
- Aceita `X-Correlation-Id` válido e o registra em `client_correlation_id`, sem trocar o nosso.
- Descarta valor inválido — longo demais, caractere fora do conjunto — e responde normalmente.

**Middleware de recover**
- Handler que entra em panic produz 500 com `SYS-INT-001` e `correlation_id`, e a resposta não contém stack trace.

### Integração — testcontainers

- Migrations aplicadas do zero produzem o schema esperado: as 7 tabelas, e cada índice e constraint nomeado acima.
- `up` seguido de `down` volta ao banco vazio — prova que os arquivos `down` não são ficção.
- Duas goroutines chamando o runner de migration ao mesmo tempo: uma aplica, nenhuma retorna erro.
- `vhook_id` decodifica os **mesmos vetores** de `testdata/vectors.json` que o teste Go usa, com e sem prefixo, em minúsculas e com caracteres ambíguos; entrada inválida levanta `22P02`. É este teste que impede as duas implementações do encoding de divergirem.
- `/readyz` responde 200 com Postgres e RabbitMQ de pé.
- Derrubar o container do Postgres produz 503 com `STO-DEP-001`; derrubar o do RabbitMQ produz 503 com `QUE-DEP-001`; `/healthz` segue 200 nos dois casos.

### Manual

```
make up          # sobe postgres e rabbitmq
make run         # sobe a api local, aplicando migrations
curl -i localhost:8080/healthz          # 200, com X-Vhook-Correlation-Id
curl -s  localhost:8080/readyz | jq     # ambos "ok"
curl -s  localhost:8080/metrics | grep vhook_build_info
docker compose stop postgres
curl -i  localhost:8080/readyz          # 503, STO-DEP-001
curl -i  localhost:8080/healthz         # ainda 200
docker compose start postgres
curl -s  localhost:8080/readyz | jq     # volta a "ready"
```

Cada linha de log sai em JSON com `correlation_id`. Mandar `-H 'X-Correlation-Id: abc-123'` faz `client_correlation_id` aparecer ao lado dele.

### CI

`make generate` seguido de `git diff --exit-code`: código gerado atrasado vira PR vermelho em vez de divergência que só aparece em runtime.

## Decisões arquiteturais geradas

- **§4.31 — Identificadores: UUIDv7 no banco, prefixo e base32 na borda.**
- **§4.32 — `events.payload` é `text` byte-exato, não `jsonb`.** Corrige a tabela de §4.5.

Ambas em [`ARCHITECTURE.md`](../../../ARCHITECTURE.md), porque valem para todo o sistema e não só para esta release. [`docs/diagrams/data-model.md`](../../../diagrams/data-model.md) é atualizado no mesmo commit.

### Correções de documento que esta spec carrega

Não são decisões novas — são divergências que apareceram ao escrever a spec e que ficariam mentindo se não fossem corrigidas junto:

- **§4.5 passa a dizer `payload text`.** Consequência direta de §4.32.
- **§4.24 perde `internal/httpapi`.** O layout de §4.24 é anterior ao layout por domínio de [`internal/CLAUDE.md`](../../../../internal/CLAUDE.md) e ficou para trás. Passa a listar `ingress`, `endpoints`, `delivery`, `ids` e `obs` como estão de fato.
- **`internal/CLAUDE.md` passa a dizer `chi`.** A regra dizia `ServeMux` da stdlib; a decisão foi revista nesta entrevista pelo motivo de §4.21 — grupos de rota com middleware distinto — e a regra segue a decisão em vez de conviver com ela.
- **`internal/ids` entra na lista de pacotes**, em `internal/CLAUDE.md`, no `CLAUDE.md` da raiz e em §4.24. `internal/obs` passa a declarar que guarda os handlers de health.

## Empurrado para o roadmap

- **Painel de observabilidade** — Prometheus e Grafana atrás de profile do compose, com o painel de §4.26. Spec própria em `platform/`, depois que existirem fila e worker.
- **Restrição de acesso a `/metrics`** — hoje o endpoint é público e expõe versão do Go e uso de memória. Enquanto a `api` roda só na máquina do desenvolvedor isso é irrelevante; a spec de deploy decide se ele fica atrás do proxy, numa porta separada ou atrás de token.
- **Geração de tipos TypeScript** — `openapi-typescript` no `make generate`, junto com o primeiro app em `apps/`.
- **Retenção** — o job periódico de §4.27. O schema já suporta (payload nullable), mas o job não existe.
