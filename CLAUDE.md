# vhook

Webhook dispatcher: recebe eventos, enfileira e entrega em endpoints HTTP cadastrados, com assinatura HMAC, timeout agressivo, retry com backoff exponencial e DLQ.

**Estado: design fechado, nenhum código escrito.** O próximo passo é a **primeira spec** — e ela começa pela entrevista (ver Workflow abaixo), não por escrever arquivo.

Pendente, em ordem: primeira spec · `docker-compose.yml` e `Makefile` · CD no Coolify apontando para o repo · opcionalmente hooks em `settings.json` para transformar "não encerrar com teste vermelho" e "quem commita é o dono" de lembrete em barreira.

**Antes de propor qualquer coisa arquitetural, leia [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).** São 30 decisões, cada uma com o tradeoff aceito e as alternativas já descartadas. Se uma sugestão sua aparece lá como descartada, o motivo está escrito — traga um argumento novo ou siga a decisão.

## Stack fechada

Go · RabbitMQ · Postgres (via sqlc) · Next.js. Não propor troca de nenhum deles sem motivo que não esteja já respondido no documento.

Regras de back em [`internal/CLAUDE.md`](internal/CLAUDE.md). Regras de front em [`apps/CLAUDE.md`](apps/CLAUDE.md), com os deltas do comercial em [`apps/commercial/CLAUDE.md`](apps/commercial/CLAUDE.md).

## Estrutura pretendida

```
cmd/{api,worker,reconciler,sink}/main.go
internal/core/       domínio puro — não importa nada
internal/ingress/    handler + service
internal/endpoints/  handler + service + repo
internal/delivery/   service + repo (consumido pelo worker)
internal/queue/      porta + adapter Rabbit
internal/dispatch/   cliente HTTP, HMAC, guard de SSRF, timeout
internal/store/      sqlc gerado, pool, advisory locks
internal/errs/       registro de erros: código, nível, status
internal/obs/        slog e métricas
contracts/           openapi.yaml + events/*.schema.json (fonte única)
i18n/                errors.{pt-BR,en,es,fr}.json — catálogo compartilhado Go + front
migrations/
apps/dashboard/      Next.js — painel
apps/commercial/     Next.js — página de vendas
packages/ui/         shadcn + componentes compartilhados entre os apps
```

## Invariantes — não quebrar sem discutir

Cada uma existe por um motivo que já foi decidido. Quebrar qualquer uma produz bug silencioso, não erro de compilação.

**Domínio**
- `internal/core` não importa Postgres, Rabbit nem `net/http`. Backoff e classificação de falha são funções puras.

**Fila**
- A mensagem carrega apenas `{delivery_id, attempt}`. Nunca o payload, a URL ou o secret — o worker busca o estado atual no banco, e é isso que faz o circuit breaker valer para o que já está enfileirado.
- Ack da mensagem original **só depois** do confirm da publicação na fila de espera. Ackar antes perde eventos.
- A DLQ recebe por publicação explícita do worker. **Nunca** configurar dead-letter na fila `deliveries` — colidiria com a escada de espera e mandaria tudo para a DLQ na primeira falha.
- O número de shards é constante em código, compartilhada por `api`, `worker` e `reconciler`. Nunca virar variável de ambiente: divergência entre processos manda mensagens para filas sem consumidor, sem erro nenhum.

**Entrega**
- O payload trafega como `[]byte` cru do ingress até o POST. Desserializar e re-serializar muda a ordem das chaves e quebra a assinatura do cliente de forma intermitente.
- 4xx é falha **permanente** (exceto 408 e 429). Só 5xx, timeout e erro de rede são retentáveis.
- Timeout de 5s por tentativa e `io.LimitReader` de 64KB na resposta. O timeout não protege de resposta gigante que chega rápido.
- Nunca seguir redirect, e validar a URL contra faixas privadas e link-local no cadastro. Sem isso o worker vira proxy de varredura da rede interna.

**Segurança**
- `applications.api_key_hash` é hasheada; `endpoints.secret` é cifrada com AES-GCM. Não é inconsistência a "corrigir": uma é verificada, a outra precisa do valor em claro para assinar.
- Payload e headers de assinatura nunca vão para log. É o vetor de vazamento mais comum, e vem sempre de um debug esquecido.
- Nunca `InsecureSkipVerify`, nem temporariamente.

**Dados e operação**
- Paginação por cursor (keyset) em `(created_at, id)`. Nunca `OFFSET` — `deliveries` é a tabela que cresce.
- Nenhuma métrica Prometheus leva `application_id` como label. Cardinalidade multiplicativa derruba o Prometheus antes do vhook; contagem por tenant vive no Postgres.
- O reconciliador roda em réplica única **e** sob advisory lock do Postgres. Réplica única é promessa de configuração, e configuração quebra.
- Migrations rodam no boot da `api` sob advisory lock.

**Erros** — contrato completo em [`docs/ERRORS.md`](docs/ERRORS.md)
- Todo erro é constante em `internal/errs` com **código, nível e status HTTP**. Formato `MOD-TYP-NNN` (`AUT-CRD-001`). Nunca `fmt.Errorf` com texto livre atravessando fronteira de API ou de serviço.
- **O registro não tem texto; o catálogo `i18n/errors.<locale>.json` não tem comportamento.** Separados para não divergirem. Código novo exige entrada em todos os locales — há teste que falha se faltar.
- Resposta de erro da API: código, `correlation_id` e `details[]`. **Nunca mensagem** — o dashboard traduz. Sem `correlation_id` não há como investigar um caso relatado.
- Payload que sai para endpoint de cliente: código **e** mensagem resolvida em `applications.locale`. O sistema dele não tem o nosso catálogo.
- O código de origem atravessa os saltos entre serviços sem ser recodificado. Recodificar perde a origem, que é o que se quer quando a falha vem de três camadas abaixo.

**Contratos** — ver [`contracts/README.md`](contracts/README.md)
- `contracts/openapi.yaml` e `contracts/events/*.schema.json` são a fonte única. Tipos Go e TS são **gerados** a partir deles.
- **Código gerado nunca é editado à mão.** Saída errada significa contrato errado: conserte o contrato e regenere. Editar o gerado funciona até a próxima geração e depois falha em silêncio.
- Contrato é editado durante a **aprovação da spec**, antes de existir código.
- Campo em schema de evento é aditivo e opcional. Remover campo ou torná-lo obrigatório quebra cliente em produção — exige `event_type` novo.

**Configuração**
- Ambiente só para segredo e endereço de infraestrutura. Comportamento do sistema em código; comportamento por tenant no banco. O teste: se adicionar um cliente exigisse deploy, está no lugar errado.

## Workflow: spec antes de código

Nada é implementado sem spec aprovada. O fluxo:

```
entrevista  →  spec  →  contratos  →  aprovação
     →  plan  →  TDD task por task  →  result  →  release
```

**Toda spec começa por entrevista: invoque a skill `brainstorming`.** Uma pergunta por vez, alternativas com tradeoff, design aprovado — e a spec é o resultado escrito disso, nunca o ponto de partida. Vale para spec pequena: se for simples mesmo, a entrevista acaba em duas perguntas.

Os contratos entram **antes** da aprovação, não depois: é aprovando a spec que request, response e payload deixam de ser prosa e viram definição executável.

- Uma spec é uma **pasta agrupada por domínio**: `docs/specs/<domínio>/NNN-name/{spec.md, plan.md, result.md}`. **Sobrescreve o default `docs/superpowers/` das skills** — o caminho não deve carregar o nome da ferramenta que gerou o documento.
- Os domínios de spec espelham os pacotes em `internal/`: uma spec de endpoints toca uma pasta.
- **O domínio agrupa, o número ordena.** Numeração global, nunca reiniciada por domínio: `spec 006` identifica sem ambiguidade. Buraco na sequência dentro de uma pasta é registro de evolução, não defeito.
- Índice, lista de domínios e regras em [`docs/specs/README.md`](docs/specs/README.md). Uma spec só está aprovada quando entra no índice. Templates em [`docs/specs/_template_/`](docs/specs/_template_/README.md).
- `result.md` registra **divergência e evidência**, nunca o que o CHANGELOG já diz. Sem divergência, uma linha dizendo isso basta.
- Cada spec vira uma release demonstrável. Se não dá para demonstrar sozinha, está grande demais — quebre.
- Skill `writing-plans` define o formato do plano. Não copiar a estrutura dela para outro lugar.
- Decisão que vale para todo o sistema vai para `docs/ARCHITECTURE.md` no formato de `_template_/architecture-decision.md`, nunca escondida dentro de uma spec de feature.
- Spec que altera schema ou fluxo **atualiza [`docs/diagrams/`](docs/diagrams/README.md) no mesmo commit**. Mermaid em markdown, nunca imagem exportada. Um conceito tem um diagrama só — antes de criar, procure se já existe.

**Nenhum placeholder em spec ou plano.** `TBD`, `a definir`, `tratar erros adequadamente` são falhas de documento. Implementar em cima de spec vaga produz a decisão por omissão, tomada por quem tem menos contexto.

## Workflow: TDD é obrigatório

**Invoque a skill `test-driven-development` antes de escrever qualquer código de produção.** Ela tem a regra completa; o resumo é: nenhum código de produção sem um teste que você **viu falhar** primeiro. Teste escrito depois passa de imediato, e isso não prova nada.

Não é regra de estilo. Este sistema falha em silêncio: uma escada de retry configurada com o TTL errado entrega tudo com o atraso errado sem lançar nenhum erro. Só teste pega isso.

O específico deste projeto:

| Camada | Ferramenta | Ciclo |
|---|---|---|
| `internal/core` | `go test`, sem container | milissegundos — é aqui que o loop red-green mora |
| `internal/{store,queue,dispatch}` | testcontainers (Postgres, RabbitMQ) | segundos |
| `apps/*` | Vitest · Testing Library · Playwright | ver [`apps/CLAUDE.md`](apps/CLAUDE.md) |

`internal/core` é puro de propósito. Se um teste seu está pedindo container para exercitar regra de negócio, o sinal é que a lógica está no pacote errado — mova para `core` antes de aceitar o container.

**Três invariantes só contam como testados por teste de integração**, nunca por unit test:

1. o retry agenda no nível certo da escada;
2. a DLQ recebe exatamente no limite de tentativas — não antes, não depois;
3. a assinatura HMAC fecha contra um **verificador independente** (o `sink`), nunca contra a mesma função que a gerou — senão o teste é tautológico e passa mesmo com o formato errado.

Nunca encerrar uma tarefa com teste vermelho.

## Convenções

- **Quem commita é o dono do repositório, nunca o agente.** Pare no ponto de commit e entregue a mensagem pronta. Não rode `git commit` nem `git push`.
- **Mensagem de commit curta:** só a linha de assunto, no imperativo, até ~60 caracteres. Corpo apenas quando o porquê não cabe no assunto — e aí uma linha, não um parágrafo. O raciocínio longo mora na spec e no `ARCHITECTURE.md`, não no histórico.
- **Conventional Commits** obrigatório — `feat:` e `fix:` alimentam o release-please, que gera CHANGELOG, tag e release. Uma versão única para o sistema inteiro.
- **Nomes de arquivo e de pasta sempre em inglês**, mesmo quando o conteúdo é em português. Exceção só para termo de domínio que não tem tradução honesta (`inscricao-municipal`, `nota-fiscal`) — traduzir esses inventa um conceito que não existe.
- **Conteúdo de documentação e mensagens de commit em português.** Código, identificadores, comentários e logs em inglês.
- Ao tomar uma decisão de arquitetura nova, registrar em `docs/ARCHITECTURE.md` no formato de `docs/specs/_template_/architecture-decision.md`.

## Documentação visual

`docs/overview.html` é publicado como Artifact em quatro páginas navegáveis:

**https://claude.ai/code/artifact/ae67af0a-46ef-46a7-9250-2cda03fefc22**

Para atualizar, **republique passando essa URL** — sem ela, uma conversa nova cria um endereço diferente em vez de atualizar o existente. O estilo mora em `docs/pages/_style.css` e é injetado inline por `bash docs/pages/build.sh`; editar o CSS dentro do HTML é editar a cópia.

É visão derivada: a fonte é `docs/diagrams/` e `docs/ARCHITECTURE.md`.

## CI

- `.github/workflows/ci.yml` — contratos, Go e front. **Cada job pergunta se o que ele testa já existe** (`go.mod`, `pnpm-workspace.yaml`) e sai limpo quando não: CI vermelho por ausência ensina a ignorar CI vermelho.
- `.github/workflows/release.yml` — release-please mantém um PR de release aberto; a versão só é cortada no merge dele. Depois disso, anuncia a release pelo próprio ingress do vhook, ou não faz nada se os secrets não existirem.
- O job de Go falha se o **código gerado dos contratos estiver atrasado**. Não regenerar deixa de ser bug de runtime e passa a ser PR vermelho.

Secrets usados pelo release, ambos opcionais: `VHOOK_INGRESS_URL` e `VHOOK_INGRESS_API_KEY`.

## Comandos

Nada ainda. Preencher quando o `Makefile` e o `docker-compose.yml` existirem — o CI espera um alvo `generate` no Makefile para checar o código gerado.
