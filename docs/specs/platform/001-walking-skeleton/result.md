# 001 — Walking skeleton · Resultado

| | |
|---|---|
| **Release** | `v0.1.0` |
| **Spec** | [`spec.md`](spec.md) |
| **Plano** | [`plan.md`](plan.md) |

---

## Divergências da spec

Nenhuma. A spec descreve comportamento observável, e todo item dela foi implementado como escrito: os três endpoints, a ordem fixa de checagem, os cinco códigos de erro, o schema com `payload text`, a função `vhook_id` e a forma externa dos identificadores.

As divergências que apareceram foram **do plano**, não da spec — o plano previa nomes de código gerado e versões de dependência que a realidade corrigiu. Estão na seção seguinte, e o plano foi atualizado em cada caso.

## Divergências do plano

| Plano dizia | Ficou | Por quê |
|---|---|---|
| Campos de enum do `openapi` são `string` | Tipo nomeado: `HealthStatus`, `ReadyStatus`, `ReadyChecksPostgres`, `ReadyChecksRabbitmq` | O `oapi-codegen` gera tipo nomeado com constante por valor. Literal sem tipo compila, mas a implementação passou a usar a constante gerada: se o enum do contrato mudar, a constante quebra a build e o literal passaria em silêncio |
| `ErrorCode` seria tipo nomeado | Alias: `type ErrorCode = string` | O gerador produz alias para schema de tipo primitivo. A conversão `openapi.ErrorCode(e.Code)` foi mantida por precaução: se o contrato promover o tipo, ela já está no lugar |
| `chi` entra na Task 9 | Entra na Task 6 | O código gerado em modo `chi-server` importa `github.com/go-chi/chi/v5`; sem a dependência, `go build ./...` falha já na geração |
| `go 1.24` no `go.mod` | `go 1.26.0` | Piso declarado pelas dependências — `testcontainers-go` e `pgx/v5` pedem acima de 1.24. O `Dockerfile` acompanhou para `golang:1.26-alpine`, senão a imagem se recusa a compilar |
| `CFG-VAL-001` registrado com o `warn` default de `VAL` | `error`, por sobrescrita explícita | A spec já dizia `error` e o plano contradizia a spec. Falta de configuração é problema de quem opera, não entrada de cliente — e `warn` num processo que morre antes de abrir a porta não é visto por ninguém. O caso entrou na tabela de `TestDeclaredOverrides`, que é onde as sobrescritas ficam travadas |
| `-race` nos alvos locais do `Makefile` | Só em `make test-race` | O detector de corrida exige `CGO_ENABLED=1` e um compilador C, que o Windows não tem por padrão. Um alvo que falha sempre na build é um alvo que se aprende a ignorar. O `-race` continua obrigatório onde importa: o CI roda `go test -race -shuffle=on ./...` direto, em Ubuntu, sem passar pelo Makefile |
| `go mod tidy` como limpeza pós-green | Necessário para compilar | Com `prometheus/client_golang`, `go get` grava o `require` sem as somas das transitivas, e a build morre em `missing go.sum entry` antes de rodar teste algum |

## Decisões que só apareceram implementando

**Ver o vermelho num pacote novo exige um stub.** Sem nenhum arquivo `.go` não-teste, o Go responde `no non-test Go files` e nunca chega a reclamar dos símbolos ausentes — que é justamente a informação que o passo vermelho existe para dar. O procedimento adotado foi criar o arquivo de implementação contendo só a linha `package X`, ver o red sobre símbolos, e só então escrever o corpo. Registrado nas Global Constraints do plano.

**Middleware de log de requisição não estava previsto.** A spec exige uma linha JSON por requisição com `correlation_id` e, ao lado, o `client_correlation_id` quando o produtor manda um válido. Só o `Recover` logava. Entrou `obs.RequestLog`, com o encadeamento `Correlation` → `RequestLog` → `Recover`, nessa ordem, para que o id já exista quando os dois últimos forem escrever.

**`rabbitCheck` disca uma conexão nova a cada probe.** Não há conexão de vida longa nesta release porque nada publica ainda. O efeito colateral é bom e ficou provado: com o RabbitMQ derrubado e trazido de volta, o `/readyz` voltou a 200 **sem reiniciar a `api`**. A conexão persistente e o reconnect nascem na spec de `queue`.

Nenhuma dessas vale para todo o sistema, então nenhuma virou seção de `ARCHITECTURE.md`. As duas que valiam já estavam previstas pela spec e entraram como §4.31 e §4.32.

## Evidência de que funciona

**Testes** — 95 em 9 pacotes, incluindo os de integração com Postgres e RabbitMQ reais:

```
ok  	github.com/victorzix/vhook/cmd/api	12.949s
ok  	github.com/victorzix/vhook/internal/errs	0.038s
ok  	github.com/victorzix/vhook/internal/ids	0.037s
ok  	github.com/victorzix/vhook/internal/obs	0.115s
ok  	github.com/victorzix/vhook/internal/store	31.351s
ok  	github.com/victorzix/vhook/internal/store/sqlc	0.037s
```

Os que provam invariante, e não só ausência de erro:

- **Teste de completude do i18n visto vermelho de propósito.** Removida a entrada `SYS-DEP-001` de `errors.fr.json`, o teste falhou com `fr: falta SYS-DEP-001 no catálogo` e nomeou locale e código. Restaurada, voltou a verde.
- **A função SQL `vhook_id` foi provada contra os mesmos vetores fixos do teste Go**, lidos do mesmo arquivo (`internal/ids/testdata/vectors.json`). Duas implementações do mesmo encoding só são seguras provadas contra uma fonte só.
- **Os vetores não foram gerados pelo código sob teste.** Foram conferidos contra uma implementação independente de base32 Crockford antes de entrarem no plano, e reconferidos depois da implementação.
- **Advisory lock:** quatro goroutines chamando o runner de migration ao mesmo tempo, nenhuma com erro.
- **`up` seguido de `down` volta ao banco vazio** — prova que os arquivos `.down.sql` não são ficção.
- **Panic não vaza:** o teste verifica que o valor do panic e o stack aparecem no log e **não** no corpo da resposta.

**Manual** — a `api` local contra a infraestrutura do compose:

```
$ curl -i localhost:8080/healthz
HTTP/1.1 200 OK
X-Vhook-Correlation-Id: 01M104JSH0EPJTCXY1T7DQ23JR
{"status":"ok"}

$ curl -s localhost:8080/readyz
{"checks":{"postgres":"ok","rabbitmq":"ok"},"status":"ready"}

$ docker compose stop postgres && curl -i localhost:8080/readyz
HTTP/1.1 503 Service Unavailable
{"error":{"code":"STO-DEP-001","correlation_id":"01M104K53FFE8BTQMFM5X4DN33",
          "details":[{"code":"STO-DEP-001","field":"postgres"}]}}

$ curl -s localhost:8080/healthz     # no mesmo instante
{"status":"ok"}
```

Liveness respondendo 200 enquanto a readiness responde 503 é o comportamento que impede um orquestrador de matar um processo saudável no primeiro blip do banco.

Os dois modos de falha de boot, exercitados por não terem cobertura automatizada:

- **Postgres fora no boot:** o processo sai com exit 1, sem abrir porta, logando `STO-DEP-001`.
- **RabbitMQ fora no boot:** a `api` **sobe** — nada publica ainda — e o `/readyz` responde 503 `QUE-DEP-001`. Trazido de volta, volta a 200 sem reiniciar a `api`.

**Observabilidade** — `vhook_build_info{commit="none",version="dev"} 1` no `/metrics`, com as coletas de runtime do Go. Nenhuma métrica leva `application_id`, e há teste verificando que a string não aparece na saída. Uma linha de log por requisição:

```json
{"level":"INFO","msg":"request","client_correlation_id":"cliente-123",
 "correlation_id":"01M104JSPGF87V6M99A8N1EH5T","method":"GET","path":"/healthz",
 "status":200,"duration_ms":0}
```

O valor mandado pelo cliente aparece em campo separado e **nunca** substitui o nosso id de rastreio.

**Determinismo do código gerado** — regerar `sqlc` e `oapi-codegen` produz arquivos byte a byte idênticos, verificado por hash. É o que faz o `git diff --exit-code` do CI valer como barreira depois do commit.

## Contratos alterados

`contracts/openapi.yaml`, editado durante a aprovação da spec:

- `paths` deixou de ser `{}` e ganhou `/healthz`, `/readyz` e `/metrics`.
- `servers` passou da raiz `/v1` para a raiz do host: as rotas operacionais vivem fora do prefixo de versão, e sobrescrita por caminho é ignorada pelo gerador de servidor.
- Schemas novos: `Health`, `Ready`, `ReadyChecks`, `ErrorBody`, `ErrorDetail`. Resposta nova: `ServiceUnavailable`.
- `ReadyChecks`, `ErrorBody` e `ErrorDetail` são schemas **nomeados** em vez de objetos embutidos, para que o nome do tipo gerado seja previsível — objeto anônimo faz o gerador inventar o nome, e o plano teria de adivinhá-lo.

Nada em `contracts/events/`: esta release não entrega payload a endpoint de cliente.

## Pendente

| Item | Para onde foi |
|---|---|
| Prometheus e Grafana com o painel de §4.26 | Spec própria em `platform/`, depois que existirem fila e worker — a parte cara é o painel, e as séries que valem gráfico não existem ainda |
| Restrição de acesso a `/metrics` | Spec de deploy. Hoje o endpoint é público e expõe versão do Go e uso de memória; enquanto a `api` roda só na máquina do desenvolvedor é irrelevante |
| Geração de tipos TypeScript | Entra com o primeiro app em `apps/`; o job de front do CI sai limpo por ausência de `pnpm-workspace.yaml` |
| Job de retenção de §4.27 | Spec futura. O schema já suporta — `events.payload` é nullable, e `NULL` significa expurgado |
| Modo de falha "migration falha no meio" | **Dívida aceita e registrada.** Marcar `dirty` e recusar todo boot seguinte é comportamento documentado do `golang-migrate`; forçar esse estado num teste exigiria uma migration deliberadamente quebrada no diretório, que passaria a rodar em toda a suíte de integração |
| `make` não instalado na máquina de desenvolvimento | Dívida do ambiente, não do código. O `Makefile` está correto e o CI o usa; localmente os comandos de dentro dos alvos foram rodados direto |
| Republicar `docs/overview.html` | Pendente. As seções §4.31 e §4.32 e o diagrama de dados mudaram, então a visão derivada está atrasada |
