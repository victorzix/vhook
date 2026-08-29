# 003 — Cadastro de endpoints

| | |
|---|---|
| **Status** | implementada |
| **Release alvo** | `v0.3.0` |
| **Plano** | [`plan.md`](plan.md) |
| **Resultado** | [`result.md`](result.md) |

---

## Problema

Existe uma `application` desde a [spec 002](../../platform/002-tenancy-bootstrap/spec.md), mas nada pode ser entregue a lugar nenhum: não há como cadastrar um destino. E uma `delivery` só existe se houver endpoint ([§4.5](../../../ARCHITECTURE.md)), então a spec de ingress está bloqueada por isto.

Segundo problema, que chega junto: estas são as **primeiras rotas que precisam ser protegidas**. As três da 001 são públicas por natureza e a 002 não expôs rota alguma. Sem autenticação, qualquer um cadastra endpoint na organização dos outros.

## Escopo

**Entra**

| Peça | Conteúdo |
|---|---|
| `internal/httpauth` | middleware de `AdminToken`, comparação em tempo constante |
| `internal/tokens` | gerador de token aleatório, extraído de `internal/apikey` |
| `internal/dispatch/ssrf.go` | checagem de faixas como **função pura**, reusada depois pelo dialer |
| `internal/secrets` | AES-GCM com AAD — não `internal/crypto`, que sombrearia o pacote da stdlib |
| `internal/endpoints` | handler, service e repo |
| `migrations/000003` | `UNIQUE (application_id, url)` |
| `contracts/openapi.yaml` | as quatro rotas e os schemas |
| `.env.example` | `VHOOK_ADMIN_TOKEN`, `VHOOK_SSRF_ALLOWLIST` |

**Não entra**

- **Taxa de sucesso 24h na listagem** — §4.21 a prevê, mas ela depende de `deliveries`, que **nunca terá linha** até a spec de entrega. Devolveria zero para todo endpoint: número que mente é pior que campo ausente.
- **`POST /:id/enable`** — o circuit breaker não existe, então nenhum endpoint consegue estar `disabled`. A rota não teria o que reativar.
- **`DELETE`** — não está em §4.21, e apagar endpoint com histórico levanta a pergunta do que fazer com as `deliveries` dele. Merece decisão própria, com o histórico já existindo.
- **Reset de `consecutive_failures` no `PATCH`** — a coluna pertence ao circuit breaker, que terá o contexto para decidir se corrigir a URL zera o contador. Decidir agora, sem ele, é decidir no escuro.
- **Rate limit** — vai para a spec de ingress, onde defende algo concreto: um produtor em loop enchendo a fila ([§4.15](../../../ARCHITECTURE.md)). No management, quem chama é o nosso BFF com token de serviço; o cenário restante é token vazado, e contra ele limitar a 10 req/s não reduz o dano de forma significativa — quem tem o token tem leitura e escrita de todos os tenants.
- **Paginação** — §4.28 limita o free a 2 endpoints. Keyset existe em §4.21 por causa de `deliveries`, que é a tabela que cresce.
- **`http://` no destino** — ver "Empurrado para o roadmap".

## Comportamento observável

### Rotas

```
POST   /v1/applications/{application_id}/endpoints
GET    /v1/applications/{application_id}/endpoints
GET    /v1/applications/{application_id}/endpoints/{endpoint_id}
PATCH  /v1/applications/{application_id}/endpoints/{endpoint_id}
```

Todas exigem `Authorization: Bearer <VHOOK_ADMIN_TOKEN>`.

O escopo do tenant vai **no caminho**, não implícito nem em header. Ver [§4.34](../../../ARCHITECTURE.md), decisão gerada por esta spec.

### Criar

```
POST /v1/applications/app_01J4PMX3R0E008000000000002/endpoints
{ "url": "https://api.cliente.com/hooks" }

→ 201
{ "id": "ept_01J4PMX3R0E008000000000003",
  "url": "https://api.cliente.com/hooks",
  "secret": "whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H",
  "status": "active",
  "created_at": "2026-08-29T14:03:11Z" }
```

### Listar

```
GET /v1/applications/app_01J4.../endpoints
→ 200
{ "endpoints": [
    { "id": "ept_01J4…", "url": "https://api.cliente.com/hooks",
      "status": "active", "created_at": "…" } ] }
```

**A listagem não traz o `secret`.** §4.28 permite revelá-lo quantas vezes se quiser, mas "permitido" não é "por padrão em toda tabela renderizada": a lista é a resposta que mais aparece em tela, em cache de front e em log de proxy. O detalhe traz.

### Detalhe

```
GET /v1/applications/app_01J4.../endpoints/ept_01J4…
→ 200  { … mesmos campos, mais "secret": "whsec_…" }
```

### Alterar

```
PATCH /v1/applications/app_01J4.../endpoints/ept_01J4…
{ "url": "https://api.cliente.com/v2/hooks" }
→ 200  { … sem "secret" … }
```

**Só `url` é mutável**, e a mudança revalida SSRF. O `PATCH` existe porque sem ele corrigir um typo exigiria recriar o endpoint — o que **gera secret novo** e obriga o cliente a se reconfigurar por causa de um erro de digitação.

`status` não é mutável aqui: desativar e reativar à mão é ciclo de vida, e pertence à spec do circuit breaker junto com a desativação automática.

### Tabela de situações

| Situação | Resultado |
|---|---|
| `POST` válido, application com 0 ou 1 endpoint | 201 com o secret |
| `POST` com a application já em 2 endpoints | 403 `RTL-LMT-001` |
| `POST` com URL já cadastrada nessa application | 409 `EPT-CFL-001` |
| `POST` com `http://` | 422 `EPT-VAL-001` |
| `POST` com URL que resolve para faixa proibida | 422 `EPT-VAL-002` |
| `POST` com host que não resolve | 422 `EPT-VAL-003` |
| `POST` sem `Authorization` ou com token errado | 401 `AUT-CRD-001` |
| `application_id` do caminho não existe | 404 `APP-NFD-001` |
| `application_id` ou `endpoint_id` malformado | 422 `SYS-VAL-001` |
| `endpoint_id` de outra application | **404** `EPT-NFD-001` |
| `PATCH` com URL inválida | mesmos códigos do `POST`, nada é alterado |

**Endpoint de outro tenant devolve 404, não 403.** A query é `WHERE id = $1 AND application_id = $2`, então "existe mas não é seu" e "não existe" são indistinguíveis de fora — 403 confirmaria a existência do recurso.

## Autenticação

Middleware que lê `Authorization: Bearer` e compara com `VHOOK_ADMIN_TOKEN` usando `crypto/subtle.ConstantTimeCompare`.

**A comparação em tempo constante não é preciosismo.** Um `==` de string em Go retorna no primeiro byte diferente, e a diferença de tempo vaza o prefixo do token, byte a byte. Com token de ambiente, que raramente rotaciona, o ataque é lento e viável.

O middleware nunca distingue "token ausente" de "token errado" na resposta: os dois são `AUT-CRD-001`. Distinguir informaria a um atacante que o formato do envio está certo.

`VHOOK_ADMIN_TOKEN` é lido do ambiente, como §4.25 já prevê. Ausência no boot mata o processo com `CFG-VAL-001`, no mesmo padrão da 001.

## Proteção contra SSRF

### O que é bloqueado

Esquema tem de ser `https`. O host é resolvido, e **todos** os IPs retornados são checados contra:

| Faixa | Motivo |
|---|---|
| `10/8`, `172.16/12`, `192.168/16` | RFC1918 — rede privada |
| `127/8` | loopback |
| `169.254/16` | link-local — é onde vive o serviço de metadados de cloud |
| `100.64/10` | CGNAT |
| `0.0.0.0/8` | não roteável |
| `::1`, `fc00::/7`, `fe80::/10` | equivalentes IPv6 |
| `::ffff:0:0/96` mapeando faixa proibida | IPv4 mapeado em IPv6 |

O último item é o driblador clássico: uma lista que só olha IPv4 deixa passar `::ffff:10.0.0.1`, que conecta em `10.0.0.1`.

Se **qualquer** IP resolvido cair numa faixa proibida, o cadastro é recusado. Não basta um IP público entre vários.

### O que esta spec promete, e o que não promete

**A validação de cadastro é conveniência e defesa em profundidade. Ela não é a garantia.**

O furo é temporal, e vale escrever para ninguém remover a checagem do disparo achando que o cadastro cobre:

1. Alguém cadastra `https://webhook.exemplo.com`, cujo A record aponta para IP público legítimo. Passa.
2. Troca o A record para `169.254.169.254`.
3. O worker resolve o DNS **de novo** no momento da entrega, e conecta no serviço de metadados.

Isso é DNS rebinding. A checagem que sustenta a garantia é no disparo, sobre o IP que a conexão vai realmente usar — um `net.Dialer` com hook `Control`, inspecionando o endereço já resolvido antes de abrir o socket. **Isso é a spec de `delivery`, não esta.**

Por isso a checagem de faixas nasce como **função pura** em `internal/dispatch`: a 003 a usa no cadastro, a spec de disparo usa **a mesma** no dialer. Duas implementações da mesma regra divergem sozinhas, e divergir aqui significa um dos caminhos deixar passar o que o outro bloqueia — sem erro, sem log.

### Host que não resolve é recusado

Se o DNS não responde, não há o que validar, e uma URL que não resolve não receberia entrega mesmo. O atrito é real — cadastrar o endpoint antes de subir o serviço — e está no roadmap.

### A exceção do `sink`

`VHOOK_SSRF_ALLOWLIST` é lista de **hostnames** separados por vírgula, comparados exatamente contra o host da URL. Host na lista pula a checagem de faixa, mas **não** pula a exigência de `https`.

Não aceita CIDR: o caso de uso é o `sink` no compose, e permitir faixa abriria um buraco maior que o problema que resolve.

## Secret do endpoint

```
whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H
└──┬─┘ └───────────────────┬────────────────────┘
   │                       └── 43 caracteres base62 — 43 × log₂(62) = 256,0 bits
   └── prefixo, reconhecível no .env de quem integra
```

Mesmo gerador da api key: sorteio de caractere direto do alfabeto com `crypto/rand`, por rejeição, sem viés de módulo. Por isso o gerador sai de `internal/apikey` para `internal/tokens` — usar um pacote chamado `apikey` para gerar secret de endpoint faria o nome mentir, e na terceira cópia seria tarde.

### No banco

```
secret_encrypted = nonce(12 bytes) || ciphertext
```

AES-256-GCM com `VHOOK_MASTER_KEY`, nonce de `crypto/rand` por operação, guardado junto — nonce não é secreto, e AES-GCM exige que seja único, nunca derivado do id nem contado.

**`endpoint_id` entra como AAD.** Um blob movido de uma linha para outra **falha na decifragem** em vez de decifrar normalmente. Fecha o cenário de alguém com escrita no banco copiar o secret de um endpoint para outro, fazendo o vhook assinar entregas de um tenant com o secret de outro. Custo: um parâmetro, e o `endpoint_id` já está em mãos porque você leu a linha.

### Consequência para §4.33

A chave mestra passa a ter **rotação assimétrica**: secrets podem ser re-cifrados (decifra com a antiga, cifra com a nova, num comando de `adminctl`), api keys **não**, porque HMAC não tem volta. Rotacionar `VHOOK_MASTER_KEY` significa: secrets sobrevivem, api keys precisam ser reemitidas. §4.33 é atualizada com isso.

## Limite do plano e idempotência

### O limite

§4.28 dá 2 endpoints por application no plano free. Na **mesma transação**:

```
SELECT … FROM applications WHERE id = $1 FOR UPDATE   -- trava o pai
SELECT count(*) FROM endpoints WHERE application_id = $1
INSERT INTO endpoints …
```

**A trava, a contagem e o insert precisam estar na mesma transação.** Contagem feita fora dela não é protegida por nada, e a corrida volta sem produzir teste vermelho — é o tipo de detalhe que uma refatoração desfaz em silêncio.

A trava é sobre a linha da **application**, então serializa toda criação daquele tenant independentemente da URL: N requisições simultâneas com URLs diferentes ainda param em 2. E é por tenant, então dois clientes criando ao mesmo tempo não se esperam.

Estouro devolve **403**, sobrescrevendo o 429 padrão do tipo `LMT`. Motivo: 429 promete que tentar de novo mais tarde resolve, e não resolve — a vaga não se libera com o tempo. O cliente ficaria em retry eterno contra uma parede.

### A idempotência

`UNIQUE (application_id, url)`. Segundo `POST` da mesma URL devolve **409 `EPT-CFL-001`**, não o endpoint existente.

Não é só deduplicação, é **regra de domínio**: dois endpoints com a mesma URL na mesma application receberiam entregas idênticas em duplicata, o que ninguém quer de propósito.

Preferi 409 a "devolve o que já existe" porque um `POST` que às vezes cria e às vezes não deixa o cliente sem saber o que aconteceu. O dashboard mostra "você já tem um endpoint para essa URL".

**Por que não `Idempotency-Key`:** §4.13 criou o header para o **ingress**, onde o produtor é uma máquina que reenvia sozinha por timeout. Aqui quem chama é o nosso BFF por causa de um humano clicando duas vezes — a chave natural resolve isso sem coluna nova, sem header e sem índice extra além do que a regra de domínio já justifica.

## Modelo de dados

Uma migration, **aditiva**:

```sql
CREATE UNIQUE INDEX endpoints_application_url_idx
    ON endpoints (application_id, url);
```

`CREATE INDEX` sem `CONCURRENTLY` porque a tabela está vazia. Nenhuma coluna nova, nenhum `ALTER`.

O índice `endpoints_application_id_idx` da 001 continua servindo a listagem.

## Erros cunhados

| Código | Quando | Nível | Status |
|---|---|---|---|
| `AUT-CRD-001` | Token administrativo ausente ou inválido | `warn` | 401 |
| `SYS-VAL-001` | Identificador malformado no caminho | `warn` | 422 |
| `APP-NFD-001` | Application do caminho não existe | `warn` | 404 |
| `EPT-VAL-001` | URL malformada ou esquema diferente de `https` | `warn` | 422 |
| `EPT-VAL-002` | URL resolve para faixa proibida | `warn` | 422 |
| `EPT-VAL-003` | Host não resolve | `warn` | 422 |
| `EPT-NFD-001` | Endpoint não existe nessa application | `warn` | 404 |
| `EPT-CFL-001` | Já existe endpoint com essa URL na application | `warn` | 409 |
| `RTL-LMT-001` | Limite de endpoints do plano excedido | `warn` | **403** |

`SYS-VAL-001` é o código que a [spec 001](../../platform/001-walking-skeleton/spec.md) deixou pendurado: ela adiou dizendo "o código nasce na spec da primeira rota que aceita um ID no caminho", e é esta. `internal/ids` deixa de devolver só erro sentinela.

Os três `EPT-VAL-*` são separados de propósito. "Sua URL não é https", "sua URL aponta para uma rede privada" e "seu domínio não resolve" produzem ações diferentes de quem está cadastrando, e um código só forçaria o dashboard a adivinhar qual mostrar.

Todos exigem entrada nos quatro locales; o teste de completude da 001 falha enquanto faltar qualquer combinação.

## Invariantes tocados

| Invariante | Como continua valendo |
|---|---|
| `endpoints.secret` é cifrado, não hasheado | AES-256-GCM. O vhook precisa do valor em claro para assinar (§4.12), então hashear impossibilitaria a entrega |
| Nunca `InsecureSkipVerify` | Esta spec não faz requisição de saída. A resolução de DNS não usa TLS |
| Validar URL contra faixas privadas e link-local no cadastro | É o núcleo da spec, com a ressalva registrada de que a garantia mora no disparo |
| Credencial nunca vai para log | O secret aparece na resposta HTTP e em nenhum log. `VHOOK_ADMIN_TOKEN` e `VHOOK_MASTER_KEY` não aparecem em lugar nenhum, nem em mensagem de erro |
| Handler não contém regra de negócio | O handler decodifica, chama o service e serializa. Trava, contagem e cifra vivem no service |
| A transação é do service, não do repo | O service abre a transação e passa o executor; o repo não sabe se está dentro de uma |
| Ambiente só para segredo e endereço | Entram `VHOOK_ADMIN_TOKEN` (segredo) e `VHOOK_SSRF_ALLOWLIST` (exceção de infraestrutura local) |
| Código gerado nunca é editado à mão | As quatro rotas entram no `openapi.yaml` antes do código, e os tipos são gerados |

Não toca: fila, ack, DLQ, escada de retry, shards, paginação keyset, métricas.

## Modos de falha

| Falha | Comportamento esperado | Observável onde |
|---|---|---|
| Duas criações simultâneas, application com 1 endpoint | A trava serializa: uma cria, a outra vê contagem 2 e devolve 403 | `RTL-LMT-001` numa das duas |
| N criações simultâneas com URLs diferentes, application vazia | Exatamente 2 são criadas; as demais 403 | idem |
| Duas criações simultâneas com a **mesma** URL | Uma cria, a outra viola o `UNIQUE` e devolve 409 | `EPT-CFL-001` |
| DNS demora a responder | Timeout de 3s na resolução; tratado como não resolve | `EPT-VAL-003` |
| Postgres cai no meio da criação | Transação: ou o endpoint existe cifrado, ou não existe. Nunca linha com `secret_encrypted` inválido | 503 `STO-DEP-001` |
| `VHOOK_MASTER_KEY` diferente da usada para cifrar | A decifragem falha na autenticação do GCM. **Não devolve lixo** | 500 `SYS-INT-001`, com o código no log |
| `secret_encrypted` movido para outra linha do banco | Decifragem falha, porque o AAD não confere | idem |
| `VHOOK_ADMIN_TOKEN` ausente no boot | Processo sai antes de abrir porta | `CFG-VAL-001`, exit ≠ 0 |
| Milhares de criações simultâneas no mesmo tenant | Não furam o limite, mas enfileiram na trava e podem exaurir o pool | É negação de serviço, não bypass; o teto vive no proxy reverso |

## Como se prova que funciona

### Unidade, sem container

**`internal/dispatch` — faixas de SSRF.** Cada faixa da tabela, com um IP dentro e um fora. **`::ffff:10.0.0.1` é caso próprio**, porque é o driblador de lista que só olha IPv4. Host da allowlist pula a faixa mas não pula o `https`.

**`internal/secrets`** — round-trip de cifra e decifra. **Decifrar com AAD de outro endpoint falha.** É o teste que prova que o AAD entra no cálculo: sem ele, uma implementação que passasse `nil` como AAD passaria em todos os outros testes, e o ganho estaria perdido em silêncio. Mesmo espírito do teste do pepper na 002.

**`internal/httpauth`** — token correto passa; ausente, vazio, com prefixo errado e incorreto todos devolvem `AUT-CRD-001` **indistinguíveis entre si**.

**`internal/tokens`** — prefixo, comprimento, alfabeto, e todos os 62 caracteres aparecendo em 10.000 gerações. Este último pega o viés de módulo, que nenhum outro teste veria.

### Integração, testcontainers

- **N goroutines criando endpoint na mesma application: exatamente 2 passam.** É o teste que prova a trava; sem ele, contagem fora da transação não daria sintoma nenhum.
- Duas criações simultâneas com a mesma URL: uma cria, a outra 409.
- **O secret decifrado do banco é igual ao que o `POST` devolveu.** É o único teste que prova que a chave entregue ao cliente é a que vai assinar — sem ele, o handler poderia devolver uma coisa e gravar outra, e só a spec de disparo descobriria, como assinatura que o cliente rejeita.
- `endpoint_id` de outra application devolve 404, e não 403.
- `PATCH` com URL inválida não altera nada — a linha continua com a URL antiga.

### Manual

Cadastrar contra o `sink` usando a allowlist, e depois tentar `http://169.254.169.254` e `https://10.0.0.1` para ver os 422 com códigos diferentes. Conferir no `psql` que `secret_encrypted` não contém o secret em claro.

## Decisões arquiteturais geradas

**[§4.34](../../../ARCHITECTURE.md) — Escopo de tenant explícito no caminho das rotas de management.** Diverge da tabela de rotas de §4.21, que lista `/v1/endpoints` plano. O motivo é o custo de plugar login depois: com escopo implícito, toda rota de management muda no dia em que existirem usuários, e o modo de falha de esquecer o escopo é servir dado de outro tenant sem erro e sem log.

## Empurrado para o roadmap

- **`allow_insecure` para permitir `http://` no destino** — booleano explícito no cadastro, **nunca default**, com aviso no front e cobertura nos termos. O campo tem de ser **persistido**, não só validado no formulário: numa investigação de vazamento, "este endpoint era `http` por opt-in explícito" precisa estar na linha do banco e no detalhe da entrega, não num submit que sumiu. Fica de fora agora porque afrouxar depois não quebra nada, enquanto apertar depois quebraria todo endpoint já cadastrado de uma vez.
- **Cadastrar endpoint cujo DNS ainda não resolve** — com um estado tipo `unverified` e revalidação. Hoje é recusa direta.
- **`DELETE /endpoints/:id`** — junto com a decisão sobre o histórico de entregas do endpoint apagado.
- **Taxa de sucesso 24h na listagem** — quando `deliveries` tiver linha.
- **`POST /:id/enable` e desativação manual** — spec do circuit breaker, que é dona de `status`, `consecutive_failures` e `disabled_at`.
- **`GET /v1/applications`** — o dashboard vai precisar para desenhar o seletor de projeto. Hoje o `application_id` sai do output do `bootstrap`.
- **Rotação de `VHOOK_MASTER_KEY`** — comando de `adminctl` que decifra e re-cifra todos os secrets. Possível para secrets, impossível para api keys.
