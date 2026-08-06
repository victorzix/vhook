# Back — regras

## Stack fechada

Go · pgx + sqlc · amqp091-go · `go-chi/chi/v5` · oapi-codegen · slog · prometheus/client_golang · testcontainers-go.

Duas escolhas foram minhas, não suas, e sinalizo:

- **Roteador é `chi`.** O que decide é §4.21: duas superfícies com middleware de autenticação distinto no mesmo processo, que em `chi.Group` são três linhas. Middleware é `func(http.Handler) http.Handler` puro e não existe tipo de contexto próprio, então nada da biblioteca aparece na assinatura de handler nenhum — a dependência fica confinada ao `main.go`. Echo e Gin foram descartados exatamente por não terem essa propriedade.
- **Validação de request vem do contrato.** O middleware do oapi-codegen valida corpo e parâmetros contra `openapi.yaml` antes do handler. Validar à mão significaria escrever duas vezes a mesma regra e deixá-las divergir.

## Layout por domínio

```
internal/
├── core/        domínio puro — não importa NADA nosso nem de terceiros
├── ingress/     handler + service
├── endpoints/   handler + service + repo
├── delivery/    service + repo (consumido pelo worker)
├── queue/       porta + adapter Rabbit
├── dispatch/    cliente HTTP, HMAC, guard de SSRF, timeout
├── store/       sqlc gerado, pool, advisory locks
├── ids/         UUIDv7 ↔ prefixo_base32
├── errs/        registro de erros
└── obs/         slog, métricas e handlers de health
```

Espelha `docs/specs/<domínio>/`: uma spec de endpoints toca uma pasta.

Os quatro de baixo não são domínio, são capacidade transversal. Dois deles merecem nota:

- **`ids` não cabe em `core`** justamente pela regra de `core`: o encoder precisa de uma biblioteca de UUID. É puro e sem I/O, mas não é domínio.
- **`obs` guarda os handlers de `/healthz` e `/readyz`**, além de `/metrics`. Liveness, readiness e métrica são a mesma superfície operacional, e nenhuma tem domínio a que pertencer. O router é montado em `cmd/api/main.go`, não dentro de um pacote.

**Direção das dependências, e ela é de mão única:** pacote de domínio importa capacidade; **capacidade nunca importa domínio**. Se `queue` precisar importar `delivery`, a porta está no lugar errado.

## As três representações do mesmo dado

| Representação | Origem | Onde pode aparecer |
|---|---|---|
| Tipo de API | **gerado** de `openapi.yaml` | só no handler |
| Struct de domínio | escrito à mão | `core` e services |
| Row / params do sqlc | **gerado** | só no repo |

**Conversão acontece só nas pontas.** O handler converte gerado ↔ domínio; o repo converte domínio ↔ sqlc. `core` nunca vê tipo gerado de nenhum dos dois lados — se um aparecer lá, a dependência inverteu.

**Nunca um struct que serve a duas dessas funções.** Reusar o tipo gerado do OpenAPI como struct de domínio parece economia e acopla a regra de negócio ao formato do JSON: mudar o nome de um campo na API passa a mudar o domínio.

## SOLID, traduzido para Go

Princípio abstrato não é verificável em review. O que é verificável:

**S — responsabilidade única.** Um pacote, um assunto. `service.go` passando de ~300 linhas normalmente tem dois serviços dentro.

**O — aberto/fechado.** Extensão por composição e por interface pequena, nunca por flag nova numa função existente. O sinal de violação é `if profile == "demo"` aparecendo em mais de um lugar — isso é comportamento que devia ser injetado.

**L — substituição.** Implementação de interface não pode exigir mais que o contrato promete. Em Go isso aparece como devolver erro num caso que a interface diz aceitar.

**I — segregação.** Interface de **1 a 3 métodos**, nomeada pelo que faz e não pela camada: `EndpointFinder`, não `EndpointRepository`. Interface com sete métodos é o cheiro.

**D — inversão.** **A interface é declarada pelo consumidor.** O service define o que precisa; o repo satisfaz sem saber que existe. Declarar a interface junto da implementação é a inversão ao contrário — costume de quem vem de linguagem com container de DI.

## Repositório

- **Interface só quando paga:** substituir em teste, ou existir mais de uma implementação. Nos outros casos o service usa o struct concreto. Como os testes de store rodam contra Postgres real via testcontainers, interface raramente compra velocidade.
- Quando existir, mora no arquivo do service, com **só os métodos que aquele service usa**.
- **Repo não contém regra de negócio.** Se um `WHERE` está codificando decisão de domínio — quais status contam como entregável, por exemplo — a decisão pertence ao service ou ao `core`, e o repo recebe o critério.
- Código gerado por sqlc nunca é editado à mão.

## Service

- Recebe struct de domínio, devolve struct de domínio ou erro.
- **Não conhece `net/http`.** Nem status, nem header, nem `http.Request`. O status vem da constante de `errs`.
- **Não conhece amqp.** Publica pela porta de `queue`.
- **A transação é do service, não do repo.** Quem conhece a fronteira da operação é quem a orquestra; o repo recebe um executor e não sabe se está dentro de transação. Repo abrindo transação própria é como uma operação de duas escritas vira meia escrita.

## Handler

Faz exatamente três coisas: decodifica no tipo gerado, chama o service, serializa a resposta. **Zero regra de negócio.**

- Erro: mapeia a constante de `errs` para status HTTP. Nunca decide status ad-hoc — é assim que o mesmo erro devolve 400 num lugar e 422 noutro.
- Nunca acessa repo direto.
- Nunca faz requisição de saída: HTTP de saída é exclusividade do `worker` via `dispatch`.

## Contexto e concorrência

- `context.Context` como **primeiro parâmetro** de tudo que faz I/O. Nunca guardado em struct.
- Cada tentativa de entrega tem o seu deadline próprio. Não herdar contexto de vida longa — é assim que o timeout de 5s deixa de valer sem ninguém notar.
- **Goroutine sempre tem dono:** quem cria sabe como ela termina. `go func()` solto, sem `WaitGroup` nem contexto de cancelamento, é vazamento esperando escala.
- Canal para comunicar, mutex para proteger estado. Canal fazendo o trabalho de mutex é complexidade sem retorno.

## Erros

Contrato completo em [`../docs/ERRORS.md`](../docs/ERRORS.md). No código:

- **Comparação sempre com `errors.Is` contra a constante**, nunca com a string da mensagem. Mensagem é texto de catálogo e muda; código é contrato.
- `fmt.Errorf` com texto livre é aceitável **dentro** de um pacote. Antes de cruzar fronteira de pacote ou de API, envolve numa constante.
- Erro que atravessa serviço mantém o código de origem. Recodificar a cada camada perde justo o que se quer saber.

## Log e métrica

- `slog` estruturado, correlation ID vindo do contexto e propagado até o worker.
- **Nunca payload, nunca header de assinatura.** É o vetor de vazamento mais comum e vem sempre de um debug esquecido.
- Nível padrão vem da constante de erro; o call site pode escalar quando o contexto justificar.
- Métrica nunca leva `application_id` como label.

## Testes

| O que | Como |
|---|---|
| `core` e lógica pura | unit, sem container, milissegundos |
| `store`, `queue`, `dispatch` | testcontainers com Postgres e RabbitMQ reais |

TDD é obrigatório: invoque a skill `test-driven-development` antes de qualquer código de produção.

Se um teste precisa de container para exercitar **regra de negócio**, a regra está no pacote errado — mova para `core` antes de aceitar o container.
