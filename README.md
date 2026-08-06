# vhook

Webhook dispatcher: recebe eventos, enfileira e entrega em endpoints HTTP cadastrados — com assinatura HMAC, timeout agressivo, retry com backoff exponencial e dead letter queue.

> **Status: em design.** Nada implementado ainda. Este arquivo é o resumo das decisões e vai ser substituído pelo README de produto quando o código começar.
>
> **O documento principal é [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md)** — cada decisão com o porquê, o tradeoff aceito e as alternativas descartadas.

---

## Contexto e critério de sucesso

O objetivo primário é **portfólio para vaga sênior**: o leitor-alvo é um tech lead lendo o repositório por 20 minutos. O que importa é arquitetura defensável, código legível e decisões explicadas — não escala real.

Objetivo secundário declarado: **existe chance de virar produto vendável**. Isso não muda o escopo do MVP, mas muda o modelo de dados — ele nasce multi-tenant, porque tenancy é barata agora e caríssima de retrofitar depois.

---

## Decisões fechadas

| Decisão | Escolha | Por quê |
|---|---|---|
| Backend | **Go** | Sinaliza perfil infra/plataforma; concorrência no worker fica natural; binário pequeno no Docker |
| Broker | **RabbitMQ** | É o que a vaga espera ver; DLQ é conceito nativo (dead-letter-exchange); TTL+DLX rende explicação não-óbvia |
| Banco | **Postgres** | Endpoints, histórico de entregas e tentativas |
| Frontend | **Next.js** | Também mira vaga fullstack; screenshot no README vende melhor |
| Tenancy | **Multi-tenant desde o schema** | `organization → application → endpoints → deliveries` |
| Login humano | **Depois, com provider** (Clerk/Auth0/Supabase) | Agora o `org_id` vem de env/header fixo. Trocar a origem dele depois é uma função. Auth própria em Go foi descartada: superfície de ataque que não agrega sinal |
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

As filas de espera **não têm consumidor**. A mensagem dorme lá até o `x-message-ttl` vencer e o Rabbit a dead-letter de volta para a exchange principal. São declaradas no boot a partir do config, então trocar o perfil de backoff é mudar uma env var.

**Backoff configurável por perfil**, porque a demo hospedada precisa ser assistível:

- produção: `1min → 5min → 15min`
- demo: `5s → 15s → 30s`

### Duas decisões que valem defesa numa entrevista

**A DLQ recebe por publicação explícita do worker, não por `nack`.** Se a fila `deliveries` tivesse dead-letter próprio, ele colidiria com a escada de espera: a mensagem cairia na DLQ na primeira falha, não na última. Publicar explicitamente deixa a intenção legível no código.

**Ordem de ack garante at-least-once.** Publisher confirms no ingress, ack manual no worker, e o ack da mensagem original só acontece *depois* do confirm da publicação na fila de espera. Se o worker morrer no meio, o evento é reprocessado. Duplicata é possível e aceitável; perda não — e é por isso que o `X-Vhook-Id` na assinatura importa, o cliente final deduplica do lado dele.

**Concorrência:** prefetch no consumer limita entregas simultâneas por worker, dimensionado junto com o pool de goroutines. Escalar = subir mais réplicas, sem coordenação.

---

## Escopo da v1

Os três pilares originais:

- **Resiliência** — retry com backoff exponencial, DLQ ao esgotar tentativas, status "Falhou" no painel
- **Segurança** — secret por endpoint, header `X-Vhook-Signature` em cada disparo
- **Anti-gargalo** — timeout curto e agressivo (~5s) no disparador HTTP

Mais os quatro extras aprovados:

- **Sink de teste embutido** — faz a demo hospedada funcionar sozinha, sem o visitante trazer URL
- **Replay manual da DLQ** — botão "reenviar" no dashboard; toda plataforma séria de webhook tem, é a primeira coisa que um entrevistador procura
- **Auto-disable de endpoint** — circuit breaker após N falhas consecutivas; um cliente morto não pode degradar a fila dos outros
- **Idempotência no ingress** — `Idempotency-Key` + unique index; resposta pronta para "e se o produtor fizer retry?"
- **Guard de SSRF** — validação de URL no cadastro e sem seguir redirects
- **Rate limit por application** — um produtor em loop não pode encher a fila dos outros
- **Shards por hash** — raio de dano limitado quando um projeto entra em carga
- **Reconciliador** — auto-cura de entregas presas; a fila é reconstruível do Postgres

### Roadmap (fora da v1, mas no README como intenção)

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

**O changelog começa no commit 1**, não quando o produto ficar utilizável. O custo de gerar é zero e o valor de portfólio está na linha do tempo: `v0.1.0 ingress enfileira` → `v0.3.0 retry com backoff` → `v0.5.0 DLQ + replay`. Um repo que nasce na `v1.0.0` com tudo pronto parece código despejado; um com 12 releases parece trabalho sustentado. `0.x` já comunica "em construção".

### O vhook anuncia as próprias releases

O workflow de release publica um evento `release.published` no ingress do **próprio vhook**, que entrega no portfólio com HMAC, timeout, retry e DLQ. O projeto é cliente de si mesmo — e qualquer bug de entrega passa a doer em quem o escreveu.

Enquanto a URL do portfólio não existir, o passo de dispatch é no-op: as releases acontecem, o CHANGELOG cresce, as tags ficam lá. Quando o vhook subir na VPS e o portfólio existir, um script lê as tags do git e publica um evento por tag histórica. Nada manual.

O backfill é seguro por causa da idempotência: `Idempotency-Key: release-v0.3.0`. Rodar duas vezes não duplica nada. Não é coincidência — é a mesma feature da v1 resolvendo o problema real do dono do sistema.

---

## Status

O design está **fechado** — as 28 decisões estão em [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md), cada uma com o tradeoff aceito e as alternativas descartadas.

Próximo passo: o plano de implementação, e então `git init` com o primeiro commit em Conventional Commits.
