# vhook

Webhook dispatcher: recebe eventos, enfileira e entrega em endpoints HTTP cadastrados — com assinatura HMAC, timeout agressivo, retry com backoff exponencial e dead letter queue.

> **Status: em design.** Nada implementado ainda. Este arquivo é o resumo das decisões e vai ser substituído pelo README de produto quando o código começar.
>
> **O documento principal é [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)** — cada decisão com o porquê, o tradeoff aceito e as alternativas descartadas.

---

## Contexto e critério de sucesso

Sistema novo, sem carga de produção e sem usuários. As restrições vêm daí: **legibilidade acima de escala**, e otimização só com medição que a justifique.

O que **não** é negociável mesmo agora é o que sai caro de retrofitar. O modelo de dados nasce multi-tenant e o formato da assinatura nasce versionado, porque mudar isso depois exige migrar dados ou quebrar todos os clientes. O resto nasce mínimo.

---

## Decisões fechadas

| Decisão | Escolha | Por quê |
|---|---|---|
| Backend | **Go** | Concorrência do worker fica idiomática; binário estático deixa a imagem Docker mínima |
| Broker | **RabbitMQ** | DLQ é conceito nativo (dead-letter-exchange), não algo que se emula; TTL+DLX resolve retry atrasado em AMQP puro |
| Banco | **Postgres** | Endpoints, histórico de entregas e tentativas |
| Frontend | **Next.js** | Ecossistema maduro para o dashboard e para o BFF que resolve token, CORS e a futura sessão |
| Tenancy | **Multi-tenant desde o schema** | `organization → application → endpoints → deliveries` |
| Login humano | **Depois, com provider** (Clerk/Auth0/Supabase) | Agora o `org_id` vem de env/header fixo. Trocar a origem dele depois é uma função. Auth própria em Go foi descartada: superfície de ataque a defender, longe de onde está o valor do sistema |
| Retry agendado | **Escada de TTL + DLX** | Rabbit vanilla, sem plugin. Plugin `delayed-message-exchange` descartado por ser community e guardar agendamento no nó; scheduler em Postgres descartado por soar contraditório com ter um broker |
| Modelagem | **`event` → `delivery` → `delivery_attempt`** | Um evento vira uma delivery por endpoint; sem isso não se responde "chegou em 3 dos 4 endpoints" |
| Conteúdo da mensagem | **Magra: só `{delivery_id, attempt}`** | Uma query no caminho quente em troca de ver o estado atual do endpoint — sem isso o auto-disable não afeta o que já está na fila |
| Assinatura | **Formato Stripe: `t=...,v1=...`** | Timestamp assinado impede replay; prefixo de versão permite rotação de secret sem quebrar clientes |
| Confidencialidade | **TLS na rede + AES-GCM no secret** | HMAC não é criptografia. Quem protege contra interceptação é o HTTPS obrigatório; cifrar o secret responde ao vazamento de banco |
| SSRF | **HTTPS obrigatório, faixas privadas bloqueadas, sem seguir redirect** | O worker roda dentro da VPS e a URL é do usuário — é a vulnerabilidade estrutural de todo dispatcher |
| Isolamento entre tenants | **64 shards fixos, hash por `application_id`** | Fila FIFO única não tem justiça. Hash por projeto (não por usuário) impede que um projeto atrase os outros do mesmo dono; o isolamento vem do prefetch por fila |
| Rede de segurança | **Reconciliador varrendo `deliveries` presas** | O Postgres é escrito antes do publish, então a fila é reconstruível a partir dele — cobre até o broker sumir |
| Plano | **`applications.plan`, default `free`** | Rate limit é necessário por si só; o campo entra junto porque o limite tem que sair de algum lugar |
| Observabilidade | **Médio** | slog estruturado com correlation ID ponta a ponta, `/metrics` Prometheus + Grafana no compose. OpenTelemetry ficou no roadmap — risco de o tracing virar o projeto |
| Testes | **Integração no caminho crítico** | testcontainers: retry agenda certo, DLQ recebe no limite, HMAC bate. Unit test só onde há lógica de verdade |
| Deploy | **VPS própria com Coolify** | Consome docker-compose direto, então o mesmo compose é dev e produção |

---

## Arquitetura

Quatro processos, cada um com uma responsabilidade e testável isolado:

| Processo | Responsabilidade | O que **não** faz |
|---|---|---|
| `api` (Go) | Ingress + CRUD de endpoints + leitura de histórico | Nunca faz HTTP de saída |
| `worker` (Go) | Consome fila, dispara POST, decide retry/DLQ | Nunca recebe requisição |
| `dashboard` (Next) | UI | Não fala com Rabbit nem Postgres |
| `sink` (Go, ~40 linhas) | Alvo-cobaia: `/ok`, `/500`, `/timeout`, `/flaky` | Só existe pra demo |

O `sink` é um serviço **separado** de propósito: o worker faz HTTP real para outro host, então a demo exercita o caminho verdadeiro em vez de um atalho interno.

### Topologia RabbitMQ

```
ingress --publish--> [ex: vhook.deliveries] --> (queue: deliveries) --> worker
                              ^                                            |
                              |                                     falhou (5xx/timeout)
                    dead-letter no vencimento                              |
                              |                                            v
                     (wait.5s / wait.15s / wait.30s              publica na wait
                      wait.1m / wait.5m / wait.15m)  <-----------------  do nível N
                       sem consumidor, x-message-ttl

                    esgotou tentativas --> [ex: vhook.dlx] --> (queue: dlq)
```

As filas de espera **não têm consumidor**. A mensagem dorme lá até o `x-message-ttl` vencer e o Rabbit a dead-letter de volta para a exchange principal. Todas são declaradas sempre no boot — fila vazia é praticamente grátis.

**Dois perfis de backoff**, constantes em código, com cada application escolhendo o seu pela coluna `applications.backoff_profile`:

- produção: `1min → 5min → 15min`
- demo: `5s → 15s → 30s`

### Duas decisões não-óbvias

**A DLQ recebe por publicação explícita do worker, não por `nack`.** Se a fila `deliveries` tivesse dead-letter próprio, ele colidiria com a escada de espera: a mensagem cairia na DLQ na primeira falha, não na última. Publicar explicitamente deixa a intenção legível no código.

**Ordem de ack garante at-least-once.** Publisher confirms no ingress, ack manual no worker, e o ack da mensagem original só acontece *depois* do confirm da publicação na fila de espera. Se o worker morrer no meio, o evento é reprocessado. Duplicata é possível e aceitável; perda não — e é por isso que o `X-Vhook-Id` na assinatura importa, o cliente final deduplica do lado dele.

**Concorrência:** prefetch no consumer limita entregas simultâneas por worker, dimensionado junto com o pool de goroutines. Escalar = subir mais réplicas, sem coordenação.

---

## Escopo da v1

Os três pilares originais:

- **Resiliência** — retry com backoff exponencial, DLQ ao esgotar tentativas, status "Falhou" no painel
- **Segurança** — secret por endpoint, header `X-Vhook-Signature` em cada disparo
- **Anti-gargalo** — timeout curto e agressivo (~5s) no disparador HTTP

Mais os extras aprovados:

- **Sink de teste embutido** — faz a demo hospedada funcionar sozinha, sem o visitante trazer URL
- **Replay manual da DLQ** — botão "reenviar" no dashboard; sem ele, falha permanente é perda de dado do cliente
- **Auto-disable de endpoint** — circuit breaker após N falhas consecutivas; um cliente morto não pode degradar a fila dos outros
- **Idempotência no ingress** — `Idempotency-Key` + unique index; o produtor também faz retry, e sem isso o evento é entregue duas vezes
- **Guard de SSRF** — validação de URL no cadastro e sem seguir redirects
- **Rate limit por application** — um produtor em loop não pode encher a fila dos outros
- **Shards por hash** — raio de dano limitado quando um projeto entra em carga
- **Reconciliador** — auto-cura de entregas presas; a fila é reconstruível do Postgres

### Roadmap

- Login humano com provider hospedado
- Billing com Stripe — a medição de uso, que é a parte cara de retrofitar, já existe no schema
- OpenTelemetry com traço distribuído ponta a ponta
- Criptografia de payload ponta a ponta (JWE), enquadrada como feature de plano pago
- Rotação de secret — o formato da assinatura já a suporta
- Event types + filtro de subscription por endpoint
- Reativação automática de endpoint desativado

---

## Versionamento e release

**Conventional Commits** (`feat:`, `fix:`, `chore:`) como fonte da verdade, **release-please** no GitHub Actions gerando o CHANGELOG, a tag e o GitHub Release.

**Uma versão única para o sistema inteiro** — `api` e `worker` compartilham o contrato de fila e nunca serão deployados separados de verdade.

**O changelog começa no commit 1**, não quando o produto ficar utilizável. O custo de gerar é zero, e `0.x` já comunica "em construção" — não há razão para esperar uma versão estável antes de registrar o que mudou.

### O vhook anuncia as próprias releases

O workflow de release publica um evento `release.published` no ingress do **próprio vhook**, que entrega num consumidor externo com HMAC, timeout, retry e DLQ. O projeto é cliente de si mesmo, então qualquer bug de entrega passa a doer em quem o escreveu.

Enquanto a URL de destino não existir, o passo de dispatch é no-op: as releases acontecem, o CHANGELOG cresce, as tags ficam lá. Quando ela existir, um script lê as tags do git e publica um evento por tag histórica. Nada manual.

O backfill é seguro por causa da idempotência: `Idempotency-Key: release-v0.3.0`. Rodar duas vezes não duplica nada — é a mesma feature da v1 resolvendo um problema real de quem opera o sistema.

---

## Status

O design está **fechado** — as 30 decisões estão em [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), cada uma com o tradeoff aceito e as alternativas descartadas.

Próximo passo: o plano de implementação, e então `git init` com o primeiro commit em Conventional Commits.
