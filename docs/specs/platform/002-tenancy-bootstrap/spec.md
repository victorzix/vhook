# 002 — Bootstrap de tenancy

| | |
|---|---|
| **Status** | implementada |
| **Release alvo** | `v0.2.0` |
| **Plano** | [`plan.md`](plan.md) |
| **Resultado** | [`result.md`](result.md) |

---

## Problema

`organizations` e `applications` existem no schema desde a [spec 001](../001-walking-skeleton/spec.md) e **nada as cria**. Não há rota, não há seed, e [§4.21](../../../ARCHITECTURE.md) deliberadamente não expõe `POST /v1/applications` — [§4.14](../../../ARCHITECTURE.md) diz que o `org_id` vem de configuração, sem login humano.

Consequência: é impossível cadastrar um endpoint ou publicar um evento, porque não existe `application` para ser dona deles. As duas specs seguintes estão bloqueadas por uma linha que não tem como nascer.

## Escopo

**Entra**

| Peça | Conteúdo |
|---|---|
| `cmd/adminctl` | subcomandos `bootstrap` e `genkey` |
| `internal/apikey` | `NewHasher`, `Generate` e `Hash` — puro, sem I/O |
| `internal/store/queries/applications.sql` | queries do `sqlc` para organização e application |
| `internal/errs` | dois códigos novos, com entrada nos quatro locales |
| `.env.example` | `VHOOK_MASTER_KEY` |

**`genkey` entra por consequência da decisão de §4.33.** Com o pepper, o bootstrap passa a exigir `VHOOK_MASTER_KEY`, e não existia forma de produzir uma. Sem o subcomando, a spec dependeria de `openssl rand -base64 32` — que não existe por padrão no Windows, onde este projeto é desenvolvido. São ~10 linhas, e é exatamente o tipo de tarefa operacional que justificou `adminctl` ser um comando com subcomandos em vez de um binário de propósito único.

**Não entra**

- **Rotação de chave** — o `--rotate` só faz sentido junto com a janela em que duas chaves são válidas ao mesmo tempo, que é mecanismo de verdade e não flag. Roadmap.
- **Checksum na chave** — o valor dele é detecção offline por scanner externo, e isso exige registrar o padrão no programa de parceiros do GitHub, o que um projeto sem usuários não tem como fazer. Sem o registro, ele compra só detecção de chave digitada errada, que o 401 já dá. E adicionar depois é retrocompatível: a validação passa a ser "se tem checksum, verifica", e chaves antigas continuam valendo.
- **Múltiplas applications por organização** — [§4.28](../../../ARCHITECTURE.md) dá 1 no plano free. A flag entra quando existir plano pago.
- **Qualquer rota HTTP** — esta spec entrega a chave; o consumo dela é a spec de ingress. Nenhuma mudança em `contracts/openapi.yaml`.
- **Migration** — nenhuma. `applications.api_key_hash` e `UNIQUE (api_key_hash)` já existem.
- **Comando para criar endpoint** — é a spec 003.

## Comportamento observável

```
$ go run ./cmd/adminctl bootstrap --org "Acme" --app "producao"
organization  org_01J4PMX3R0E008000000000001  Acme
application   app_01J4PMX3R0E008000000000002  producao
              plan=free  locale=pt-BR  backoff_profile=production
api key       vhk_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H
              ↑ aparece uma única vez. Não é recuperável.
```

```
$ go run ./cmd/adminctl bootstrap
error: APP-CFL-001
exit 1
```

```
$ go run ./cmd/adminctl genkey
sMHwOHXe5b3Rz3nJHFXhBBJUsPHXsUvyCF+7cAKBQvY=
```

`genkey` imprime 32 bytes de `crypto/rand` em base64 padrão, no formato que `VHOOK_MASTER_KEY` espera. Não escreve em arquivo nenhum e não toca o banco: quem roda decide onde a chave vai morar.

Flags, todas com default:

| Flag | Default | Validação |
|---|---|---|
| `--org` | `vhook` | não vazia, ≤ 200 caracteres |
| `--app` | `default` | não vazia, ≤ 200 caracteres |
| `--locale` | `pt-BR` | um de `pt-BR`, `en`, `es`, `fr` |
| `--backoff-profile` | `production` | `production` ou `demo` |

`plan` não é flag: o `CHECK` da coluna só aceita `free` hoje. Não é omissão — acrescentar uma flag de um valor único seria inventar configuração.

| Situação | Resultado |
|---|---|
| Banco vazio | Cria as duas linhas numa transação, imprime as três linhas acima, exit 0 |
| Já existe qualquer organização | `APP-CFL-001`, exit 1, **nada tocado** |
| `--locale` fora dos quatro | `APP-VAL-001`, exit 1, **antes** de abrir transação |
| `--backoff-profile` fora dos dois | `APP-VAL-001`, exit 1, antes de abrir transação |
| `--org` ou `--app` vazia | `APP-VAL-001`, exit 1, antes de abrir transação |
| `DATABASE_URL` ausente | `CFG-VAL-001`, exit 1 |
| `VHOOK_MASTER_KEY` ausente | `CFG-VAL-001`, exit 1, **antes** de abrir transação |
| `VHOOK_MASTER_KEY` não é base64 válido, ou não tem 32 bytes | `CFG-VAL-001`, exit 1, nomeando a variável |
| Postgres inalcançável | `STO-DEP-001`, exit 1 |

Os ids impressos usam a forma externa de [§4.31](../../../ARCHITECTURE.md): `org_` e `app_` mais base32 dos 128 bits do UUIDv7. É o mesmo valor que `vhook_id()` decodifica no `psql`.

## Modelo de dados

**Nenhuma migration.** O schema da 001 já tem tudo: `organizations`, `applications` com `api_key_hash text NOT NULL` e `UNIQUE (api_key_hash)`, e os `CHECK` de `plan`, `locale` e `backoff_profile`.

As duas inserções acontecem **numa transação só**. Sem ela, uma falha entre os dois inserts deixaria uma organização sem application — estado que o comando recusaria corrigir na execução seguinte, porque a organização já existiria.

### A transação não basta: a exclusão precisa de advisory lock

A checagem "já existe organização?" é um **read-modify-write**, e em `READ COMMITTED` — o default do Postgres — duas execuções simultâneas leem `count = 0` e ambas inserem. O `UNIQUE (api_key_hash)` **não** as serializa: cada uma gera uma chave diferente, então nunca colidem. E `organizations` não tem constraint que sirva de ponto de serialização.

Por isso a transação toma `pg_advisory_xact_lock` **antes** do `count`, com um identificador constante em código. A segunda execução bloqueia; ao passar, vê a organização que a primeira commitou e devolve `APP-CFL-001`. O lock é liberado no fim da transação, sem cleanup explícito.

É o **terceiro** uso de advisory lock no projeto, pelo mesmo motivo dos outros dois: as migrations no boot da `api` ([§4.24](../../../ARCHITECTURE.md)) e o reconciliador. O padrão é o mesmo — quando a correção depende de "ninguém mais está fazendo isso agora", a promessa vem do banco e não da configuração.

## Formato e hash da api key

```
vhk_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H
└┬┘ └───────────────────┬────────────────────┘
 │                      └── 43 caracteres do alfabeto base62
 └── prefixo fixo
```

**43 caracteres, e o número não é arbitrário:** `43 × log₂(62) = 256,0` bits. É o mesmo orçamento de entropia que o raciocínio de hash e de colisão usa, expresso no alfabeto em vez de em bytes.

A geração sorteia **caractere direto do alfabeto** com `crypto/rand`, usando rejeição para não ter viés de módulo — não é conversão de base de 32 bytes. Base62 não é potência de dois, então converter bytes exigiria aritmética de inteiro grande; sortear do alfabeto dá o mesmo resultado em dez linhas.

O que vai para `applications.api_key_hash` é

```
HMAC-SHA256(VHOOK_MASTER_KEY, chave)
```

**Determinístico, e isso é exigência do schema.** O ingress recebe a chave num header e precisa achar a application a partir dela em toda requisição do caminho quente. Com `UNIQUE (api_key_hash)` e hash determinístico isso é `WHERE api_key_hash = $1` num índice. Com bcrypt, argon2 ou salt aleatório por linha, esse `WHERE` não existe: seria preciso varrer todas as applications e comparar uma a uma, a cada requisição.

**Com pepper, e não apenas SHA-256.** O pepper — a chave mestra que vive no ambiente e nunca no banco — faz um dump de banco lido ser inútil sozinho: sem ele não há como confirmar se uma chave conhecida corresponde a um hash guardado. Custa 0,6 µs contra uma query de Postgres de centenas de microssegundos na mesma requisição.

O raciocínio completo, os ganhos nomeados e o custo de rotação estão em [§4.33](../../../ARCHITECTURE.md), decisão gerada por esta spec.

## Onde a lógica mora, e por que não dentro de `cmd/`

`internal/apikey` é pacote próprio, transversal, ao lado de `ids` e `errs`:

```go
// Hasher holds the pepper. Built once at boot so the key is validated in one
// place instead of at every call site.
type Hasher struct{ /* … */ }

// NewHasher validates the master key and returns errs.MissingConfig when it is
// absent or not 32 bytes.
func NewHasher(masterKey []byte) (*Hasher, error)

// Generate returns a fresh key and its hash. The plaintext is returned once
// and never stored.
func (h *Hasher) Generate() (plain, hash string, err error)

// Hash is deterministic: the ingress calls it on an incoming key to find the
// application by indexed lookup.
func (h *Hasher) Hash(plain string) string
```

**Por que um `Hasher` com estado, e não `Hash(masterKey, plain string)`.** A chave mestra é validada uma vez, no boot, em vez de a cada chamada — e não existe call site capaz de esquecer de passá-la, ou de passar `nil` por acidente e produzir um HMAC com chave vazia que ninguém percebe até a autenticação falhar em produção.

O motivo de o pacote existir é a spec de ingress. Se a geração e o hash morassem dentro de `cmd/adminctl`, o ingress reimplementaria o hash — e duas implementações do mesmo hash divergem sozinhas, que é exatamente o problema que o teste de vetores compartilhados da 001 existe para impedir. Aqui o risco é pior que lá: divergência no hash não dá erro, dá 401 em chave válida.

`cmd/adminctl` fica fino: valida flags, chama `apikey.Generate`, insere as duas linhas numa transação, imprime.

**`cmd/adminctl` com subcomando, e não `cmd/bootstrap` de propósito único**, porque vão aparecer mais comandos operacionais — replay da DLQ, desativar endpoint, expurgo de retenção. Criar o lugar agora custa um `switch` sobre `os.Args[1]`; criar depois significa mover o primeiro comando e quebrar o que já estiver documentado.

## Erros cunhados

| Código | Quando | Nível | Status |
|---|---|---|---|
| `APP-CFL-001` | Bootstrap já executado: existe organização | `warn` | 409 |
| `APP-VAL-001` | Flag com valor fora do permitido | `warn` | 422 |

Ambos com entrada nos quatro locales de `i18n/errors.<locale>.json`; o teste de completude da 001 falha enquanto faltar qualquer combinação.

O status HTTP não é usado por um comando de terminal. Ele existe na constante porque `APP-CFL-001` reaparece numa rota no dia em que o dashboard puder criar application — e é [§4.29](../../../ARCHITECTURE.md) que exige que o status viva na constante, para o mesmo erro não devolver 400 num handler e 422 noutro.

## Invariantes tocados

| Invariante | Como continua valendo |
|---|---|
| `api_key_hash` é hasheada, não cifrada | HMAC-SHA256, e o `UNIQUE` da coluna é o que **exige** o determinismo. Continua sendo verificação, nunca recuperação: o valor em claro não é derivável do que está guardado, ao contrário de `endpoints.secret`, que é cifrado porque precisa ser recuperado para assinar (§4.12) |
| Credencial nunca vai para log | A chave em claro vai para o **stdout do comando** e mais nada. Não passa pelo `slog`, não vira campo estruturado, não é gravada em disco. A **chave mestra** também não: ela é lida do ambiente e nunca aparece em saída, nem em mensagem de erro — `CFG-VAL-001` nomeia a variável, jamais o valor |
| Ambiente só para segredo e endereço | Flags de CLI não são ambiente. O comando lê exatamente dois: `DATABASE_URL` e `VHOOK_MASTER_KEY`, ambos segredo ou endereço. `genkey` não lê nenhum |
| Nomes de arquivo e pasta em inglês | `cmd/adminctl`, `internal/apikey` |
| Nenhum `fmt.Errorf` de texto livre cruzando fronteira | Toda saída de erro do comando é uma constante de `errs` |

Não toca: fila, entrega, assinatura HMAC, SSRF, paginação keyset, métricas. Nenhum deles tem código ainda.

## Modos de falha

| Falha | Comportamento esperado | Observável onde |
|---|---|---|
| Postgres cai no meio das inserções | Transação: ou as duas linhas existem, ou nenhuma. Nunca organização órfã | exit ≠ 0, `STO-DEP-001` |
| Chave gerada e falha antes do commit | A chave nunca foi impressa e a transação não fechou. Rodar de novo é seguro | exit ≠ 0 |
| Colisão de `UNIQUE (api_key_hash)` | O comando **falha** em vez de sobrescrever. Colisão silenciosa daria duas applications com a mesma chave, e a segunda roubaria os eventos da primeira | exit ≠ 0, violação de constraint no log |
| Alguém roda contra o banco de produção sem querer | Recusa, porque já existe organização. É o motivo de o default ser recusar em vez de ser idempotente | `APP-CFL-001` |
| Duas execuções simultâneas em banco vazio | **Advisory lock de escopo de transação**, tomado antes do `count`: a segunda bloqueia, e ao passar vê a organização que a primeira commitou e devolve `APP-CFL-001`. Nunca duas organizações | `APP-CFL-001` em todas menos uma |
| Terminal com o stdout redirecionado para arquivo | A chave vai para o arquivo. **É responsabilidade de quem roda** — o comando não tem como distinguir, e mascarar a chave tornaria o comando inútil | — |
| `VHOOK_MASTER_KEY` trocada depois do bootstrap | **Todas as api keys param de ser verificáveis**, e não há como recuperá-las: o HMAC exige o valor em claro, que não guardamos. O caminho é reemitir chave para cada tenant. É o custo registrado em §4.33, não um bug | 401 em chave que era válida |
| `VHOOK_MASTER_KEY` presente mas diferente da usada no bootstrap | Mesmo efeito acima. É o motivo de o teste de integração comparar o hash gravado com o recalculado: um ambiente com a chave errada é indistinguível de um bug de hash sem esse teste | 401 em chave válida |

## Como se prova que funciona

**Unidade** — `internal/apikey`, sem container:

- Formato: prefixo `vhk_`, exatamente 43 caracteres depois dele, todos no alfabeto base62.
- `Hash` é determinístico: a mesma chave com a mesma chave mestra produz a mesma saída em execuções diferentes do processo.
- Chaves diferentes produzem hashes diferentes.
- **A mesma chave com chaves mestras diferentes produz hashes diferentes.** É o teste que prova que o pepper está de fato dentro do cálculo. Sem ele, uma implementação que ignorasse a chave mestra e fizesse SHA-256 puro passaria em todos os outros testes — e o ganho inteiro de §4.33 estaria perdido em silêncio, sem nenhum sintoma.
- `NewHasher` devolve `errs.MissingConfig` com chave `nil`, vazia, e de tamanho diferente de 32 bytes. Chave curta aceita em silêncio seria HMAC com entropia menor que a anunciada.
- 10.000 chaves geradas, nenhuma repetida. É o teste que pega a fonte de entropia errada — um `math/rand` semeado com constante passaria em todos os outros.
- Distribuição do alfabeto: sobre 10.000 chaves, todos os 62 caracteres aparecem. Pega o viés de módulo que a amostragem por rejeição existe para evitar — uma implementação com `% 62` sobre bytes favoreceria os primeiros caracteres, e nenhum outro teste notaria.
- `Hash` de string vazia não entra em pânico e não devolve string vazia.

**Integração** — testcontainers:

- Bootstrap em banco vazio cria exatamente uma organização e uma application, com `plan=free`, `locale=pt-BR`, `backoff_profile=production` nos defaults.
- **O hash gravado em `api_key_hash` bate com `Hasher.Hash(chave impressa)`, usando a mesma chave mestra.** É o único teste que prova que a chave impressa é utilizável: sem ele, o comando poderia imprimir uma chave e gravar o hash de outra, e ninguém descobriria até a spec de ingress recusar uma chave válida com 401.
- **O mesmo bootstrap rodado com chave mestra diferente produz hash diferente no banco.** Fecha por integração o que o teste de unidade do pepper fecha por unidade: prova que a chave mestra atravessa o comando inteiro até a coluna, e não fica parada num parâmetro que ninguém usa.
- Segunda execução devolve `APP-CFL-001` e a contagem de linhas nas duas tabelas não muda.
- Falha injetada entre os dois inserts deixa o banco vazio — prova a transação.
- Flags inválidas falham **sem** criar linha nenhuma.

**Manual**

```
docker compose up -d postgres rabbitmq
go run ./cmd/api                            # aplica migrations
go run ./cmd/adminctl genkey                # chave mestra, para o .env
go run ./cmd/adminctl bootstrap             # imprime as três linhas
go run ./cmd/adminctl bootstrap             # APP-CFL-001, exit 1
psql "$DATABASE_URL" -c "SELECT id, name, plan FROM applications"
```

E o cenário que prova o pepper de fora: apagar as duas linhas, trocar `VHOOK_MASTER_KEY` por outra do `genkey`, rodar o bootstrap de novo com a **mesma** flag `--app`, e comparar os dois `api_key_hash`. Devem ser diferentes.

O `SELECT` mostra o `uuid` cru; `vhook_id('app_01J4…')` faz o caminho de volta a partir do id impresso.

**Demonstrabilidade, com honestidade:** esta release não é exercitável por HTTP, porque a chave só passa a ser aceita na spec de ingress. O que a 002 demonstra é o bootstrap em si — comando roda, chave aparece uma vez, segunda execução recusa, e o hash no banco confere com a chave impressa. É pouco para uma demo visual e é o preço de ter separado tenancy de cadastro de endpoint.

## Decisões arquiteturais geradas

**[§4.33](../../../ARCHITECTURE.md) — Formato e hash da api key.** Registra o formato `vhk_` + 43 caracteres em base62 e, principalmente, **por que hash lento está errado aqui** — é o tipo de decisão que alguém tenta "corrigir" em review trocando SHA-256 por bcrypt, sem o contexto do índice único e do caminho quente.

## Empurrado para o roadmap

- **Rotação de api key** — janela com duas chaves válidas, e o `--rotate` como interface. O formato da chave já a suporta.
- **Rotação da chave mestra sem reemitir api key** — hoje é impossível: recalcular o HMAC exige o valor em claro. O caminho seria versionar o pepper (`v1:hash`) e aceitar os dois durante uma janela, mas isso só resolve para chaves **novas**; as antigas continuariam presas ao pepper antigo, que teria de continuar existindo. Dívida registrada em §4.33, não escondida.
- **Checksum na chave** — quando houver registro num scanner de segredo que o consuma. Retrocompatível.
- **Múltiplas applications por organização** — junto com plano pago.
- **Guardar os últimos 4 caracteres da chave** para o dashboard poder exibir `vhk_…D8H` numa lista. Exige coluna nova, e não há tela ainda.
- **Mais subcomandos de `adminctl`** — replay da DLQ, desativar endpoint, expurgo de retenção. Cada um na spec do seu domínio.
