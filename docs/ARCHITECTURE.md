# vhook — Arquitetura e decisões de projeto

Documento de decisões. Cada seção segue o mesmo formato: **o que foi decidido**, **por quê**, e **o que se abriu mão em troca**. Onde uma alternativa foi descartada, ela está registrada com o motivo — decisão sem alternativa descartada não é decisão, é default.

> Status: design fechado, implementação não iniciada.

---

## 1. O problema

Entregar webhooks é enganosamente difícil. A parte fácil é o `POST`. A parte difícil é que **o destino não é confiável e não é seu**: o servidor do cliente cai, demora 40 segundos, responde 500 por meia hora, ou some. Um dispatcher ingênuo trava, perde eventos, ou martela um endpoint morto até degradar a entrega de todos os outros clientes.

O vhook resolve isso com três garantias:

1. **Nada se perde** — o evento é aceito e persistido antes de qualquer tentativa de entrega.
2. **Falha temporária não é falha** — reentrega com backoff exponencial, e DLQ só no fim da linha.
3. **Um cliente ruim não contamina os outros** — timeout agressivo, isolamento por fila e desativação automática de endpoint problemático.

## 2. Contexto e restrições

Sistema novo, sem carga de produção e sem usuários. Duas consequências guiaram as escolhas de forma consistente: não há dado para dimensionar nada, e o que sai caro de retrofitar precisa nascer certo mesmo assim.

- **Legibilidade acima de escala.** Otimizações que só importam acima de milhares de eventos por segundo foram deliberadamente omitidas. Cada componente precisa ser compreensível numa leitura.
- **Modelo de dados nasce certo, features nascem mínimas.** Multi-tenancy e formato de assinatura versionado estão no dia 1 porque retrofitar quebra clientes ou exige migração dolorosa. Login, billing e tracing distribuído ficaram fora porque plugam depois sem tocar no que existe.
- **Sem biblioteca dona da semântica central.** Retry, backoff e DLQ *são* o produto. Delegá-los a uma biblioteca que resolve tudo pronto significa que ajustar a política depois é discutir com as opiniões dela — e é justamente a parte que mais vai precisar de ajuste.

---

## 3. Visão geral

Quatro processos, cada um com uma responsabilidade única e testável isolado:

| Processo | Responsabilidade | O que **não** faz |
|---|---|---|
| `api` (Go) | Ingress de eventos, CRUD de endpoints, leitura de histórico | Nunca faz HTTP de saída |
| `worker` (Go) | Consome a fila, dispara o POST, decide retry/DLQ | Nunca recebe requisição |
| `dashboard` (Next.js) | Interface | Não fala com Rabbit nem com Postgres |
| `sink` (Go, ~40 linhas) | Alvo-cobaia: `/ok`, `/500`, `/timeout`, `/flaky` | Só existe para a demo |

```
                       ┌──────────────┐
   produtor  ──POST──> │     api      │ ──> Postgres (persiste evento + deliveries)
                       │  (ingress)   │ ──> RabbitMQ (publica delivery_id)
                       └──────────────┘
                              │ 202 Accepted (imediato)
                              ▼
                       ┌──────────────┐
                       │    worker    │ ──> Postgres (busca payload/url/secret,
                       │ (dispatcher) │      grava cada tentativa)
                       └──────────────┘
                              │ POST assinado, timeout 5s
                              ▼
                       endpoint do cliente
```

**Stack:** Go, RabbitMQ, Postgres, Next.js. Deploy via docker-compose em VPS própria com Coolify.

**Por que Go:** concorrência do worker fica idiomática (`context.WithTimeout` por tentativa, pool de goroutines limitado por prefetch), e o binário estático deixa a imagem Docker minúscula. **Tradeoff:** o dashboard fica em outra linguagem, então o repositório tem dois mundos em vez de tipos compartilhados de ponta a ponta.

---

## 4. Decisões

### 4.1 RabbitMQ como broker

**Decisão.** RabbitMQ carrega a fila; Postgres carrega estado e histórico.

**Por quê.** DLQ é conceito nativo no Rabbit (dead-letter-exchange), não algo que se emula. E o requisito de reentrega atrasada tem uma solução elegante em AMQP puro (§4.2) que rende explicação de verdade.

**Tradeoff.** Uma dependência de infraestrutura a mais no compose, e AMQP tem curva de aprendizado. Nesta escala, um `SELECT ... FOR UPDATE SKIP LOCKED` em Postgres resolveria o mesmo problema com uma peça a menos — é a escolha que eu faria num sistema onde o broker fosse a única razão para ter Rabbit rodando.

**Descartado.** Redis + Asynq entregaria retry, DLQ e web UI prontos, mas colocaria a semântica central do produto dentro de uma biblioteca (§2). NATS JetStream tem backoff nativo mais limpo, porém é bem menos difundido: menos material operacional, menos gente com experiência e mais risco na hora de depurar comportamento estranho em produção.

### 4.2 Retry atrasado com escada de TTL + DLX

**Decisão.** Uma fila de espera por nível de atraso, sem consumidor, com `x-message-ttl` e `x-dead-letter-exchange` apontando de volta para a exchange principal.

```
ingress --publish--> [ex: vhook.deliveries] --> (queue: deliveries) --> worker
                              ^                                            │
                              │                                    falhou (5xx/timeout)
                    dead-letter no vencimento                              │
                              │                                            ▼
                     (wait.5s / wait.15s / wait.30s              publica na wait
                      wait.1m / wait.5m / wait.15m)  <-----------------  do nível N
                       sem consumidor, x-message-ttl

                    esgotou tentativas --> [ex: vhook.dlx] --> (queue: dlq)
```

**Por quê.** RabbitMQ não tem "entregue isso em 5 minutos". A mensagem dorme numa fila que ninguém consome, e o próprio broker a devolve quando o TTL vence. Funciona em Rabbit vanilla, sem plugin, e o agendamento é responsabilidade do broker — não há timer na aplicação para se perder num restart.

Os dois perfis são constantes em código, e **todas** as filas de espera são declaradas sempre — seis filas vazias não custam nada (§4.19). Cada application escolhe o seu pela coluna `applications.backoff_profile`, o que faz demo e produção coexistirem no mesmo deploy em vez de exigirem configuração de ambiente diferente:

- **produção:** 1min → 5min → 15min
- **demo:** 5s → 15s → 30s

**Tradeoff.** Cada valor de atraso distinto exige a sua própria fila — atraso arbitrário por mensagem é impossível. Com um conjunto pequeno e fixo de níveis isso não incomoda; um sistema que precisasse honrar `Retry-After` com precisão de segundo teria que trocar de abordagem.

**Descartado.** O plugin `rabbitmq_delayed_message_exchange` aceita atraso arbitrário por header e simplificaria o código, mas é community, exige imagem customizada e mantém os agendamentos no nó — fraqueza operacional real. Um scheduler varrendo `next_attempt_at` em Postgres daria atraso arbitrário e agendamento consultável, ao custo de mais um processo, polling, e da contradição de usar o banco para o trabalho que motivou ter um broker.

### 4.3 A DLQ recebe por publicação explícita, não por `nack`

**Decisão.** Ao esgotar as tentativas, o worker publica na exchange de dead letter. A fila `deliveries` não tem dead-letter próprio.

**Por quê.** Não é estilo, é correção. Se `deliveries` tivesse `x-dead-letter-exchange`, ele colidiria com a escada de espera: um `nack` mandaria a mensagem para a DLQ **na primeira falha**, não na última. A escada depende de a falha ser tratada com republicação, não com rejeição.

**Tradeoff.** Perde-se o descarte automático do broker, então o worker precisa acertar essa lógica. Em compensação a intenção fica explícita no código, onde é lida.

### 4.4 Ordem de ack: at-least-once, nunca at-most-once

**Decisão.** Publisher confirms no ingress, ack manual no worker, e o ack da mensagem original acontece **depois** do confirm da publicação na fila de espera.

**Por quê.** Ackar antes de publicar cria uma janela em que uma morte do worker perde o evento silenciosamente. Na ordem correta, a mesma morte causa reprocessamento.

**Tradeoff.** Duplicata é possível: se o worker morre entre publicar e ackar, o cliente recebe a entrega duas vezes. Esse é o tradeoff consciente — em entrega de webhook, duplicata é um incômodo que o consumidor resolve, perda é um bug que ninguém percebe. É por isso que `X-Vhook-Id` é estável entre tentativas: o cliente deduplica do lado dele.

### 4.5 Modelagem em três níveis

**Decisão.** `event` → `delivery` → `delivery_attempt`.

```
event (1) ──fanout──> delivery (N) ──tentativas──> delivery_attempt (M)
"o que aconteceu"     "para quem"                  "cada POST individual"
```

```
organizations       id, name
applications        id, org_id, name, api_key_hash, plan, backoff_profile,
                    locale
endpoints           id, application_id, url, secret_encrypted, status,
                    consecutive_failures, disabled_at
events              id, application_id, event_type, payload jsonb,
                    idempotency_key, received_at
                    UNIQUE (application_id, idempotency_key)
deliveries          id, event_id, endpoint_id, status, attempt_count,
                    next_attempt_at, completed_at
delivery_attempts   id, delivery_id, attempt_number, status_code,
                    response_time_ms, response_snippet, error, attempted_at
```

**Por quê.** Um evento vira uma delivery por endpoint inscrito. Sem essa separação é impossível responder "esse evento chegou em 3 dos 4 endpoints" — que é exatamente a pergunta que o operador tem ao abrir o painel. Colapsar evento e entrega numa tabela só forçaria duplicar payload por destino e destruiria a noção de "o mesmo evento".

`deliveries.status` ∈ `pending | delivering | succeeded | failed | dead`. `dead` é o estado de DLQ, separado de `failed` porque o replay manual só faz sentido a partir dele.

**Tradeoff.** Três tabelas em vez de uma, e `delivery_attempts` é a que cresce mais rápido no sistema — precisa de política de retenção antes de virar problema.

### 4.6 Mensagem magra: a fila carrega referência, não dados

**Decisão.** A mensagem contém apenas `{delivery_id, attempt}`. O worker busca payload, URL e secret no Postgres a cada tentativa.

**Por quê.** Uma query no caminho quente é o preço de **ver o estado atual**. Se o endpoint foi editado ou desativado pelo circuit breaker depois do enfileiramento, o worker respeita isso. Com mensagem gorda, o auto-disable simplesmente não teria efeito sobre o que já está na fila — e é justo o cenário em que ele mais importa: um endpoint que acabou de ser desativado por estar em colapso tem uma fila cheia de mensagens antigas apontando para ele.

Efeito colateral bem-vindo: a fila não vira storage de payload, e o Rabbit fica com uso de memória previsível independente do tamanho dos eventos.

**Tradeoff.** Uma leitura no banco por tentativa, e o Postgres passa a estar no caminho crítico da entrega. Num volume alto isso vira o gargalo, e a saída seria cache do endpoint com invalidação — complexidade que não se justifica agora.

### 4.7 Assinatura HMAC no formato do Stripe

**Decisão.**

```
X-Vhook-Id: evt_01HQ...
X-Vhook-Timestamp: 1754438400
X-Vhook-Signature: t=1754438400,v1=9f86d081884c7d...
```

Assinatura = `HMAC_SHA256(secret, "{timestamp}.{raw_body}")`.

**Por quê, ponto por ponto:**

- **O timestamp entra dentro do que é assinado**, não só num header solto. É isso que impede replay: o cliente rejeita se `|now - t| > 5min`, e o atacante não consegue reassinar com timestamp novo sem o secret. Assinar apenas o body deixaria qualquer chamada capturada válida para sempre.
- **O prefixo `v1=` num formato de lista** permite enviar duas assinaturas durante uma janela de rotação de secret. A rotação em si é roadmap, mas o formato nasce pronto — retrofitar isso quebraria todos os clientes de uma vez.
- **A assinatura é sobre os bytes crus do body.** Se o worker desserializasse e re-serializasse o JSON, a ordem das chaves mudaria e a verificação do cliente falharia de forma intermitente e impossível de depurar. O payload trafega como `[]byte` do ingress até o POST.

**Tradeoff.** Formato compatível com Stripe significa também herdar suas limitações, como a ausência de assinatura assimétrica — o cliente precisa guardar um secret compartilhado, e não apenas verificar uma chave pública.

### 4.8 Classificação de falha: 4xx não é retentável

| Resposta | Decisão |
|---|---|
| 2xx | sucesso |
| 3xx | falha **permanente** — redirect não é seguido, ver §4.11 |
| 4xx | falha permanente |
| 408, 429 | retenta; honra `Retry-After` se razoável |
| 5xx | retenta |
| timeout, DNS, conn refused, erro de TLS | retenta |

**Por quê.** Retry existe para falha transitória. Um 401 ou 422 é o cliente rejeitando de propósito — insistir três vezes só queima worker, polui o histórico e atrasa entregas legítimas na fila. Essa distinção é a diferença entre implementar retry e entender retry.

**Tradeoff.** Um cliente que responde 400 durante um deploy quebrado perde o evento sem reentrega automática. É o que o replay manual da DLQ existe para cobrir.

### 4.9 Timeout de 5s e leitura limitada da resposta

**Decisão.** `context.WithTimeout` de 5 segundos por tentativa, e `io.LimitReader` de 64KB na resposta, com os primeiros 2KB guardados em `response_snippet`.

**Por quê.** O timeout é o que impede um cliente lento de engarrafar a fila inteira: sem ele, um endpoint que demora 40s ocupa um slot de worker por 40s. O limite de leitura cobre um caso que o timeout **não** resolve — um cliente que responde 200 com 500MB de HTML. A resposta está chegando dentro do prazo, só é enorme, e a memória do worker acaba antes.

**Tradeoff.** 5 segundos é agressivo e vai reprovar endpoints legitimamente lentos. Isso é intencional: o contrato de um consumidor de webhook é responder rápido e processar de forma assíncrona. Num produto real isso seria configurável por endpoint com um teto rígido.

### 4.10 Circuit breaker por endpoint

**Decisão.** Após N falhas consecutivas, o endpoint é marcado `disabled` e para de consumir worker. `consecutive_failures` zera em qualquer sucesso.

**Por quê.** Isolamento de vizinho barulhento. Um cliente que ficou fora do ar por dois dias geraria retries indefinidamente, consumindo slots de worker que pertencem a clientes saudáveis. O painel mostra o endpoint desativado e o motivo, então a desativação é diagnóstico, não punição silenciosa.

**Tradeoff.** Um endpoint desativado precisa de reativação manual — e se o dono não olhar o painel, ele fica desligado. A alternativa (reativação automática por sondagem periódica) é roadmap.

### 4.11 Proteção contra SSRF

**Decisão.** No cadastro do endpoint: exigir `https` e resolver o DNS rejeitando faixas privadas e link-local. No disparo: **não seguir redirects**. O `sink` de teste é exceção explícita via allowlist no ambiente.

**Por quê.** É a vulnerabilidade estrutural de todo dispatcher de webhook e vale nomeá-la: o usuário cadastra uma URL arbitrária, e o worker executa **dentro da sua rede**. Nada impede alguém de cadastrar `http://169.254.169.254/latest/meta-data/` para ler credenciais de metadados de cloud, ou `http://localhost:5432` para sondar o banco. O vhook viraria um proxy de varredura interna, autenticado e com retry automático.

Não seguir redirect é a metade menos óbvia: sem isso, o atacante cadastra uma URL pública válida que responde 302 para o alvo interno, e escapa inteiramente da validação de cadastro.

**Tradeoff.** Endpoints legítimos atrás de redirect param de funcionar, e `http://` puro é proibido mesmo em rede confiável. Ambos são restrições que eu manteria em produção.

### 4.12 Confidencialidade: TLS na rede, AES-GCM no banco

**Decisão.** HTTPS obrigatório (§4.11) para confidencialidade em trânsito. `endpoints.secret` cifrado com AES-GCM por chave mestra vinda do ambiente. Payload e headers de assinatura numa allowlist de campos nunca-logados.

**Por quê.** Vale separar três propriedades que costumam ser confundidas:

| Propriedade | Quem resolve |
|---|---|
| Autenticidade — "veio do vhook" | HMAC |
| Integridade — "não foi alterado" | HMAC |
| **Confidencialidade — "ninguém no meio leu"** | **TLS** |

A assinatura não esconde nada: o payload vai em texto claro e assinado. Quem interceptar consegue ler perfeitamente — só não consegue forjar. Então quem protege contra interceptação é o TLS, e o `http.Client` do Go valida cadeia de certificados por padrão. O cuidado real é nunca ligar `InsecureSkipVerify` "só para testar".

**Duas credenciais, em direções opostas, guardadas de formas diferentes.** `applications.api_key_hash` é **hasheada**, porque o cliente a usa para publicar eventos *dentro* do vhook e só é preciso verificar se bate. `endpoints.secret` é **cifrada** e não hasheada, porque o vhook precisa do valor em claro para calcular o HMAC do que *sai* — hasheá-la impossibilitaria assinar. Não é inconsistência: é a diferença entre uma credencial que se verifica e uma que se usa.

Cifrar o secret no banco responde a uma ameaça diferente e mais provável que interceptação: um dump de banco vazado entrega a capacidade de forjar webhooks assinados de **todos** os clientes. E a allowlist de log existe porque o jeito mais comum de vazar payload de webhook não é interceptação — é um `log.Printf("%v", req)` que ficou de um debug.

**Tradeoff.** A chave mestra no ambiente é um único ponto de falha, sem rotação nem KMS. Num produto real seria envelope encryption com KMS gerenciado.

**Descartado por ora.** Criptografia de payload ponta a ponta — o endpoint registra uma chave pública e recebe o corpo em JWE, o que protegeria até de um proxy TLS-terminating do lado do cliente. O custo decide sozinho: **todo** consumidor precisaria implementar decifragem para receber qualquer coisa, o que mata a adoção. É por isso que nenhum player grande faz isso por padrão. Fica no roadmap enquadrado como feature de plano pago, para cliente com exigência de compliance.

### 4.13 Idempotência no ingress

**Decisão.** Header `Idempotency-Key` opcional, com `UNIQUE (application_id, idempotency_key)`. Colisão retorna 202 com o `event_id` já existente, sem criar nada.

**Por quê.** O produtor também faz retry. Se ele não recebeu o 202 por timeout de rede, vai reenviar — e sem idempotência o evento é entregue duas vezes a todos os endpoints. É a resposta pronta para a pergunta clássica de "e se o produtor reenviar?".

Uso concreto pelo próprio projeto: o backfill de releases (§4.16) usa `release-v0.3.0` como chave, então rodar o script duas vezes por engano não duplica nada.

**Tradeoff.** Só funciona se o produtor colaborar enviando a chave. Sem ela, o comportamento é o antigo — o que é aceitável, já que a alternativa (deduplicar por hash de payload) causaria falsos positivos em eventos legitimamente idênticos.

### 4.14 Multi-tenancy desde o schema, login depois

**Decisão.** `organization → application → endpoints → deliveries` no dia 1. Nenhum login humano: o `org_id` vem de configuração, atrás de uma única função.

**Por quê.** Assimetria de custo. Tenancy no schema é praticamente grátis agora e exigiria migração de todos os dados depois. Login é o inverso: caro agora, e plugável depois trocando a origem do `org_id` por um provider hospedado (Clerk/Auth0/Supabase).

**Tradeoff.** A demo pública não tem signup, então cada visitante vê a mesma organização. Aceitável para o objetivo.

**Descartado.** Autenticação própria em Go — sessões, hash de senha, recuperação. Seria superfície de ataque a manter e defender, longe de onde está o valor do sistema: resiliência de entrega, não gestão de credencial de usuário, que é problema resolvido por terceiros melhor do que eu resolveria.

### 4.15 Plano e rate limit

**Decisão.** `applications.plan` com default `'free'`, e rate limit no ingress derivado dele.

**Por quê.** O rate limit é necessário por si só — um produtor em loop não pode encher a fila e degradar todo mundo. O campo `plan` entra junto porque o limite tem que sair de algum lugar, e "de uma coluna" é tão barato quanto "de uma constante".

Billing de verdade fica de fora: Stripe, checkout e planos pagos plugam depois sem tocar no schema. Mas vale registrar o que é realmente caro de retrofitar, e **não é o billing — é a medição de uso**. Não se fatura evento que nunca foi contado, e consumo passado é irrecuperável. Isso já está resolvido: `events` e `deliveries` com `application_id` e timestamp são a base de qualquer cobrança futura.

**Tradeoff.** Existe uma coluna sem uso real de negócio hoje, e os limites do plano free são números escolhidos sem dados de uso.

### 4.16 Versionamento: release-please, e o vhook anuncia as próprias releases

**Decisão.** Conventional Commits como fonte da verdade, release-please no GitHub Actions gerando CHANGELOG, tag e GitHub Release. Versão única para o sistema inteiro. O workflow de release publica um evento `release.published` no ingress do **próprio vhook**, que o entrega num consumidor externo.

**Por quê.** Versão única porque `api` e `worker` compartilham o contrato de fila e nunca serão deployados separados de verdade — versioná-los em separado criaria a ilusão de independência que não existe.

O changelog começa no primeiro commit, não quando o produto ficar utilizável: o custo de gerar é zero e `0.x` já comunica "em construção", então esperar por uma versão estável não compra nada.

O dogfooding é o ponto: qualquer bug de entrega passa a doer em quem escreveu o dispatcher, e o caminho de entrega passa a ser exercitado a cada release em vez de só em teste. Enquanto a URL de destino não existir o passo é no-op, e um script de backfill publica um evento por tag histórica quando ela existir — seguro por idempotência (§4.13).

**Tradeoff.** Dependência circular: se o vhook estiver fora do ar durante uma release, o anúncio não sai. Falha benigna e reenviável pela DLQ, mas é uma dependência real assumida em troca de exercitar o próprio caminho de entrega continuamente.

### 4.17 O `sink` é um serviço separado

**Decisão.** O alvo-cobaia da demo roda como processo próprio no compose, não como rotas dentro da `api`.

**Por quê.** Assim o worker faz HTTP real para outro host, e a demo exercita o caminho verdadeiro — resolução de DNS, conexão, timeout de rede. Rotas internas na própria API criariam um atalho que não prova nada.

**Tradeoff.** Um container a mais no compose, que existe apenas para demonstração.

### 4.18 Observabilidade: o suficiente para ver a fila

**Decisão.** `slog` estruturado com correlation ID atravessando ingress → fila → worker, `/metrics` Prometheus e Grafana no compose. Testes de integração com testcontainers no caminho crítico: o retry agenda no nível certo, a DLQ recebe exatamente no limite, o HMAC fecha. Unit test onde há lógica de verdade.

**Por quê.** O correlation ID é o que torna possível reconstruir a vida de um evento a partir dos logs, atravessando processos. As métricas existem porque o gráfico da fila crescendo é o que comunica visualmente que este é um sistema assíncrono — e o mesmo painel é o instrumento real de diagnóstico.

Os testes se concentram nos três invariantes que, se quebrarem, tornam o sistema inútil sem que nada pareça errado.

**Tradeoff.** Sem OpenTelemetry, não há traço distribuído ponta a ponta com waterfall de latência. Ficou no roadmap por risco de escopo: é fácil o tracing virar o projeto e o dispatcher ficar pela metade.

### 4.19 Isolamento entre tenants: shards por hash de `application_id`

**Decisão.** Um conjunto **fixo** de 64 filas (`deliveries.0` … `deliveries.63`), routing key = `hash(application_id) % 64`. Todo worker consome **todas** as filas, com prefetch modesto por fila.

**Por quê.** Fila FIFO única não tem noção de justiça: quem publica depois de um backlog de 50.000 eventos espera atrás dos 50.000, por mais workers que existam — o gargalo é a posição, não a capacidade. Sharding limita o raio de dano: um burst congestiona apenas o shard daquele projeto.

Três detalhes que fazem esse desenho funcionar:

- **O hash é por `application_id` (projeto), não por `org_id` (usuário).** Os 5 projetos de um mesmo usuário caem em shards diferentes, então um projeto em carga não atrasa os outros do mesmo dono. Hashear por usuário reintroduziria exatamente o problema.
- **O isolamento vem do prefetch por fila, não de particionar consumidores.** Como o prefetch é por consumidor-por-fila, um worker inscrito em 64 filas com prefetch 3 reserva 3 slots para cada shard. Um burst no shard 7 ocupa os 3 slots do shard 7 e não toca nos outros 189. Isso elimina a necessidade de atribuir shards a workers — todo worker consome tudo, e escalar é subir réplica.
- **`shards × prefetch` é o limite de entregas em voo por worker.** 64 × 3 = 192, confortável em Go com timeout de 5s. É esse produto que dimensiona o worker, não o número de shards isolado.
- **O número de shards é constante em código, não variável de ambiente.** Se `api`, `worker` e `reconciler` divergirem nesse número, mensagens vão para o shard errado e ninguém percebe — não há erro, só entregas presas em filas que nenhum consumidor esperava. Constante compilada torna a divergência impossível. E como mudar o número exige drenar e migrar de qualquer forma (ver abaixo), poder trocá-lo sem deploy não compraria nada.

**Por que o número é fixo e generoso, e não proporcional à base.** A tentação é escalar o número de shards com a quantidade de projetos — 3 projetos, 2 shards; 5.000 projetos, 256 shards. Não funciona, por dois motivos:

1. **Rehashing.** Ir de 16 para 32 shards muda o destino de quase toda application, e as mensagens que já estão nas filas antigas ficam no shard errado. Corrigir exige drenar e migrar com consumo duplo durante a janela. Hash consistente reduz a quantidade de chaves que se movem, mas não elimina a drenagem. É por isso que o Kafka obriga a escolher o número de partições na criação e recomenda superprovisionar: **o número de shards é decisão de uma vez, o número de consumidores é decisão contínua.**
2. **Fila vazia é praticamente grátis.** Uma fila sem mensagens custa alguns KB e um processo Erlang. Não existe o desperdício que a proporcionalidade tentaria evitar — o custo real está nos consumidores, e esses já escalam de forma independente.

E o superprovisionamento se comporta melhor do que o cálculo proporcional justamente quando a base é pequena: com 3 projetos e 64 shards, cada projeto fica quase certamente sozinho no seu shard — **isolamento perfeito, de graça**. Conforme a base cresce, a interferência aumenta de forma gradual (5.000 projetos ≈ 78 por shard) em vez de exigir uma migração a cada degrau.

**Tradeoff.** Projetos que colidem no mesmo shard ainda se afetam, e a colisão é sorteada, não escolhida — um tenant grande pode cair junto de um pequeno. A mitigação seletiva é fila dedicada (§5, roadmap), que é também uma feature natural de plano enterprise.

**Descartado.** Fila por application dá isolamento verdadeiro e escala proporcional de verdade, sem shard vazio nem rehashing. O custo não é desperdício, é ciclo de vida: criação e destruição dinâmica de filas, descoberta de filas novas pelos workers, e milhares de processos de fila no broker conforme a base de tenants cresce. Vale como upgrade seletivo, não como default. Fair queuing em Postgres (round-robin entre applications via `DISTINCT ON`) daria justiça real com peso por plano, mas reintroduz o scheduler descartado em §4.2 e deixa o Rabbit decorativo.

### 4.20 Reconciliador: o Postgres é a rede de segurança

**Decisão.** Um processo periódico varre `deliveries` presas em `pending` ou `delivering` além de um limite de tempo e republica na fila.

**Por quê.** Como a mensagem é magra (§4.6), a linha em `deliveries` é escrita **antes** do publish. Isso torna o Postgres a fonte da verdade, e abre a possibilidade de auto-cura em três cenários que os publisher confirms não cobrem:

- o publish falhou ou o confirm nunca chegou, e o evento existe no banco mas não na fila;
- o worker morreu no meio de uma entrega, deixando `delivering` para sempre;
- o RabbitMQ perdeu a fila inteira — nó recriado, volume errado, erro de operação.

Nesse último caso a fila é integralmente reconstruível a partir do banco, o que é a resposta concreta para "e se o broker cair?". Vale mais que qualquer configuração de durabilidade, porque cobre erro humano além de falha de máquina.

**Tradeoff.** Republicação pode duplicar uma entrega que na verdade estava em andamento — mais uma manifestação consciente do at-least-once de §4.4. O limite de tempo precisa ser folgado o suficiente (bem acima do timeout de 5s) para que o caso normal nunca seja reconciliado.

### 4.21 Duas APIs, um binário

**Decisão.** Ingress e management são grupos de rota com middleware de auth distintos no mesmo processo `api`.

```
POST /v1/events            Bearer <api_key>, Idempotency-Key
                           → 202 { event_id, deliveries: 3 }

GET    /v1/endpoints                    lista com taxa de sucesso 24h
POST   /v1/endpoints                    cria e retorna o secret
PATCH  /v1/endpoints/:id
POST   /v1/endpoints/:id/enable         reativa após circuit breaker
GET    /v1/deliveries?status=&endpoint_id=&cursor=
GET    /v1/deliveries/:id               detalhe + timeline de tentativas
GET    /v1/deliveries/stream            SSE do feed
POST   /v1/deliveries/:id/replay        só quando status = dead
GET    /v1/usage
GET    /healthz  /readyz  /metrics
```

**Por quê.** Os dois têm perfis opostos — o ingress é público, quente e autenticado por chave de aplicação; o management é interno, frio e autenticado por token administrativo. A separação que importa é a de autenticação e a de contrato, e essa existe. Separar em dois processos só se paga quando os perfis de tráfego divergirem o suficiente para escalarem em ritmos diferentes.

**Paginação por cursor (keyset) em `(created_at, id)`, nunca `OFFSET`.** `deliveries` é a tabela que cresce, e `OFFSET 50000` obriga o Postgres a varrer 50.000 linhas para descartá-las. Com keyset o custo é constante em qualquer página.

**Tradeoff.** Um bug no management pode derrubar o ingress, que é o caminho crítico. Mitigado por serem grupos de rota isolados e pelo management ter tráfego desprezível.

### 4.22 O dashboard é um BFF, não um cliente

**Decisão.** O browser nunca fala com a API Go. Ele chama route handlers do Next, que chamam a API server-side com `VHOOK_ADMIN_TOKEN`.

**Por quê.** Resolve três problemas com uma decisão: o token administrativo nunca chega ao browser, CORS deixa de existir, e a troca de "org fixa do ambiente" para "org da sessão" fica confinada a uma camada que já existe. Sem o BFF seria preciso escolher entre expor o token e construir essa camada depois — e a segunda opção é a mesma coisa, só mais tarde e sob pressão.

**Tradeoff.** Um salto de rede a mais por requisição do dashboard, e a lógica de autorização passa a viver em TypeScript em vez de Go.

### 4.23 Feed em tempo real por SSE

**Decisão.** `GET /v1/deliveries/stream` com Server-Sent Events. O feed atualiza no instante em que uma tentativa é registrada.

**Por quê.** Não é enfeite: o produto **é** assíncrono, e ver o evento aparecer como `pending`, virar `failed`, esperar e virar `succeeded` sozinho é a única forma de a interface comunicar o que o sistema faz. Com polling de 3s a mesma história acontece em passos discretos que parecem uma tabela recarregando.

SSE em vez de WebSocket porque o tráfego é unidirecional — o servidor empurra, o cliente nunca fala. WebSocket traria handshake, framing e keepalive para resolver um problema que não existe aqui, e SSE reconecta sozinho por padrão no browser.

**Tradeoff.** Conexões de longa duração para gerenciar, e o feed precisa de fallback para polling se a conexão cair além do retry automático. Uns 30 minutos de trabalho a mais que polling puro.

### 4.24 Layout: domínio puro no centro

```
vhook/
├── cmd/{api,worker,reconciler,sink}/main.go
├── internal/
│   ├── core/       domínio puro: política de backoff, classificação de falha
│   ├── store/      Postgres via sqlc
│   ├── queue/      porta + adapter Rabbit  ← isola o degrau 4 (§5)
│   ├── dispatch/   cliente HTTP, HMAC, guard de SSRF, timeout
│   ├── httpapi/    handlers de ingress e management
│   ├── errs/       registro de erros: código, nível, status HTTP
│   └── obs/        slog e métricas
├── contracts/      openapi.yaml + events/*.schema.json (fonte única)
├── i18n/           errors.<locale>.json — catálogo compartilhado Go + dashboard
├── migrations/
├── apps/dashboard/ Next.js
└── docker-compose.yml
```

**`internal/core` não importa Postgres, Rabbit nem `net/http`.** A política de backoff e a classificação de falha retentável (§4.8) são funções puras: entram estado e resposta, sai a próxima ação. Testam em milissegundos, sem container, e são exatamente as duas regras cuja quebra silenciosa arruinaria o sistema.

**O reconciliador é binário próprio com réplica única, e ainda assim protegido por advisory lock do Postgres.** Como goroutine dentro do worker, cada réplica republicaria as mesmas entregas presas — o processo criado para curar duplicação seria a maior fonte dela. O lock existe porque "réplica única" é promessa de configuração, e configuração quebra.

### 4.25 Configuração: ambiente só para segredo e endereço

**Decisão.** Vai para variável de ambiente apenas o que é **segredo** ou **endereço de infraestrutura**. Comportamento do sistema vive em código; comportamento por tenant vive no banco.

| Onde | O que | Por quê |
|---|---|---|
| Ambiente | `DATABASE_URL`, `RABBITMQ_URL`, `VHOOK_MASTER_KEY`, `VHOOK_ADMIN_TOKEN`, `VHOOK_SSRF_ALLOWLIST`, `VHOOK_API_URL` | Muda por ambiente, nunca em runtime |
| Código | número de shards, prefetch, timeout de 5s, perfis de backoff, limite de tentativas, teto do `LimitReader` | Divergência entre processos seria bug silencioso; mudança exige deploy de qualquer forma |
| Banco | `applications.plan`, `applications.backoff_profile`, limites do plano, estado do endpoint | Muda por tenant e em runtime |

**Por quê.** A lista de ambiente encurta sozinha quando se aplica esse critério, e três candidatos óbvios saem: o perfil de backoff virou coluna (§4.2), os limites de plano viraram coluna (§4.15), e o número de shards virou constante — este último porque divergir entre `api` e `worker` produziria mensagens em filas que ninguém consome, sem erro nenhum (§4.19).

Na prática são **três valores compartilhados pelos serviços Go** (`DATABASE_URL`, `RABBITMQ_URL`, `VHOOK_MASTER_KEY`), mais um exclusivo da `api`, um do `worker` e dois do dashboard. No compose isso é uma âncora YAML declarada uma vez e referenciada por serviço; no Coolify, variáveis no nível do projeto. O `sink` não recebe nenhuma.

**Tradeoff.** Ajustar timeout ou prefetch exige rebuild em vez de restart. Aceitável: são números que só deveriam mudar com medição, e medição vem com deploy.

### 4.26 Métricas sem label de tenant

**Decisão.** `vhook_events_received_total`, `vhook_deliveries_total{status}`, `vhook_delivery_duration_seconds` (histograma por classe de status), `vhook_queue_depth{shard}`, `vhook_dlq_depth`, `vhook_endpoints_disabled`, `vhook_reconciler_republished_total`. **Nenhuma leva `application_id`.**

**Por quê.** Cardinalidade em Prometheus é multiplicativa: mil tenants × seis métricas × labels de status viram dezenas de milhares de séries temporais, e o Prometheus cai antes do vhook. Contagem por tenant vive no Postgres, que é feito para isso — e é de lá que sai a medição de uso de §4.15. É o erro mais comum de instrumentação em sistema multi-tenant.

Painel Grafana: taxa de entrega, latência p50/p95/p99, profundidade por shard, DLQ e endpoints desativados. Prometheus e Grafana ficam atrás de um profile do compose, para que `docker compose up` continue leve e o VPS possa dar a memória ao Rabbit se precisar.

### 4.27 Retenção

**Decisão.** `delivery_attempts` por 30 dias. `deliveries` por 90, exceto `dead`, que fica até resolução. `events.payload` anulado após 30 dias, metadados preservados indefinidamente. Job periódico com `DELETE` em lotes.

**Por quê.** Anular o payload e manter o resto é deliberado: o payload é ao mesmo tempo o dado sensível do cliente e o volumoso, enquanto os metadados são baratos e são a base da medição de uso. Também é o que sustenta a afirmação de não guardar dado de cliente além do necessário, que é conversa de LGPD.

**Tradeoff.** `DELETE` em lotes causa bloat e locks longos quando a tabela fica grande. Particionar `delivery_attempts` por mês e trocar a limpeza por `DROP PARTITION` é o degrau — e particionar com volume zero seria a otimização prematura recusada em todo o resto deste documento.

### 4.28 Limites do plano free

1 application por organization, 2 endpoints por application, 10.000 eventos/mês, 10 requisições/s no ingress com burst de 50, histórico de 5 dias.

Números escolhidos sem dado de uso. Registrados como arbitrários e revisáveis, não como estudo — e é por isso que vivem no banco (§4.25) e não em código.

O secret do endpoint pode ser revelado quantas vezes se quiser. A indústria mostra uma única vez, mas isso só é aceitável quando existe rotação: sem ela, perder o secret obrigaria a recriar o endpoint e reconfigurar o cliente. Muda para mostrar-uma-vez no mesmo momento em que a rotação entrar.

### 4.29 Erros são constantes com código; a mensagem depende de quem é dono da UI

**Decisão.** Todo erro é uma constante que carrega **código, nível e status HTTP**. O código tem o formato `MOD-TYP-NNN` (`AUT-CRD-001`). A taxonomia completa está em [`ERRORS.md`](ERRORS.md).

Três superfícies com contratos diferentes:

| Superfície | O que vai | Quem traduz |
|---|---|---|
| API → dashboard | código, `correlation_id`, `details[]` — **sem mensagem** | o front, via catálogo i18n |
| vhook → endpoint do cliente | código **e** mensagem já resolvida | o vhook, no idioma de `applications.locale` (default `pt-BR`) |
| log e métrica | código, nível e todo o detalhe técnico | ninguém, é interno |

**Por quê.** O princípio que decide as três linhas é o mesmo: **quem traduz é quem é dono da interface.** O dashboard é nosso, então o catálogo vive no front e a API não carrega texto — trocar uma frase deixa de ser deploy de backend. O sistema do cliente não vai implementar o nosso catálogo, então mandar só código ali empurraria trabalho para quem integra; a mensagem é resolvida antes de sair.

Dois efeitos colaterais que valem por si:

- Código estável separa contrato de texto. A mensagem pode ser reescrita sem quebrar cliente nenhum, e o cliente pode ramificar lógica em cima do código com segurança.
- Mensagem não vaza detalhe interno por acidente. Num sistema onde o worker fala com a rede interna (§4.11), `dial tcp 10.0.0.5:5432: connect: connection refused` numa resposta de API é vazamento de topologia.

**O nível vive na constante como padrão, não como verdade.** Um endpoint de cliente respondendo 503 é `warn`; o mesmo endpoint na enésima falha consecutiva, disparando o circuit breaker, é `error`. A constante define o default para que o nível não seja decidido ad-hoc em cada call site; o call site pode escalar.

**O status HTTP também vive na constante.** Sem isso o mesmo erro devolve 400 num handler e 422 noutro, e a inconsistência só aparece para quem integra.

**Registro e catálogo são artefatos separados, e é isso que impede divergência.** O registro em `internal/errs` tem comportamento e nenhum texto; o catálogo em `i18n/errors.<locale>.json` tem texto e nenhum comportamento, indexado por código. Um catálogo por locale, consumido pelos dois lados: `go:embed` no Go, import direto no dashboard. Um teste garante que todo código registrado tem entrada em todo locale — é o tipo de furo que passa em review e aparece em produção como mensagem vazia.

A v1 tem **um único locale, `pt-BR`**. A coluna existe para que adicionar idioma depois seja um arquivo novo, não uma migração — e o teste de completude é justamente o que torna essa adição segura, porque ele falha enquanto o novo locale estiver incompleto.

**Propagação entre serviços: o código original atravessa.** Um serviço que recebe erro de outro repassa o código de origem e **não** o recodifica. Recodificar em cada salto perde a origem, que é exatamente a informação que se quer quando algo falha três camadas abaixo. Só se cria código novo quando a semântica muda de verdade — e aí o original vai para o log.

**Tradeoff.** A API pública devolvendo só código é mais estrita que o padrão de mercado: Stripe manda código e mensagem, GitHub manda mensagem. Quem integra no ingress precisa consultar o catálogo para entender `ING-VAL-002`, o que é atrito real na primeira integração. O preço é pago com catálogo público em `ERRORS.md` — e comprado de volta em liberdade para reescrever texto sem versionar API.

Custo secundário: toda condição de erro nova exige uma constante e uma entrada por locale antes de compilar limpo. É burocracia deliberada — é ela que impede o `fmt.Errorf("algo deu errado")` de virar contrato por descuido.

**Descartado.** Mensagem no backend com i18n server-side para as duas superfícies: centralizaria o texto, mas obrigaria deploy de backend para corrigir uma frase do dashboard e mandaria o idioma do usuário no request. Código sem nível nem status na constante: deixaria as duas decisões para o call site, que é onde a inconsistência nasce.

### 4.30 Contrato antes de código, e o código é gerado a partir dele

**Decisão.** `contracts/openapi.yaml` (API REST) e `contracts/events/*.schema.json` (payloads que saem) são fonte única. Tipos Go (`oapi-codegen`) e TS (`openapi-typescript`) são gerados. Os contratos são editados durante a **aprovação da spec**, antes de existir implementação.

**Por quê.** Dois formatos porque são duas naturezas: OpenAPI descreve endpoints que nós expomos; os payloads entregues são requisições que **nós fazemos** contra o servidor de outra pessoa, o que OpenAPI não modela — e JSON Schema, além de modelar, serve de documentação para quem integra.

Gerar os tipos nos dois lados é o que torna drift entre front e back impossível em vez de improvável. E editar o contrato antes do código é o que faz a implementação ser rápida: os tipos já existem quando o primeiro teste é escrito.

**Código gerado nunca é editado à mão**, e há teste verificando que o gerado está em dia com o contrato. Sem esse teste, "esqueci de regenerar" produz divergência que só aparece em runtime — exatamente o que ter contrato deveria impedir.

**Tradeoff.** Toda mudança de superfície passa a exigir editar YAML, regenerar e commitar o gerado — atrito real em mudança pequena. E ferramenta de codegen tem opiniões: o formato dos tipos produzidos não é o que se escreveria à mão, o que às vezes obriga a adaptar o contrato ao gerador em vez do contrário.

**Descartado.** Contrato em markdown descritivo: rápido de escrever, mas nada valida e nada gera, então ele diverge do código em silêncio — e contrato em que se confia estando errado é pior que contrato nenhum. Contrato dentro da pasta de cada spec: `/v1/endpoints` não pertence a uma feature, então a definição da API ficaria espalhada em N pastas, cada uma congelada no dia em que foi escrita. Snapshot por spec junto do contrato vivo: a cópia desatualizada conviveria com a atual e alguém leria a errada — o histórico do git já preserva a intenção original.

---

## 5. Escada de escala

O design de hoje sustenta na ordem de **centenas de entregas por segundo** num VPS modesto (5 réplicas, prefetch 3 por shard, entrega média de 200ms ≈ 750/s, ~65M/dia). Os degraus acima disso estão mapeados, e cada um só é subido quando a métrica pedir:

| Degrau | O que muda | Quando |
|---|---|---|
| 1 | Mais réplicas de worker | Fila crescendo com CPU sobrando |
| 2 | Cap de concorrência por application no worker | Um tenant monopolizando slots do shard dele |
| 3 | Fila dedicada para o tenant grande | Um cliente específico exigindo isolamento contratual |
| 4 | Kafka particionado por `application_id` | O broker deixou de ser suficiente |

### Quanto custa o degrau 4 (Rabbit → Kafka)

O hash já ser por `application_id` faz a **chave de partição** transferir sem tradução — mas dois mecanismos não transferem, e são justamente os mais centrais do design.

**Transfere sem custo:** toda a lógica de negócio. Classificação de falha retentável, assinatura HMAC, timeout, `LimitReader`, circuit breaker, idempotência, modelagem em três níveis, o reconciliador e o Postgres como fonte da verdade. Isso é a maior parte do sistema.

**Precisa ser reescrito:**

1. **A escada de TTL + DLX (§4.2) não tem equivalente.** Kafka não tem TTL por mensagem nem dead-letter exchange. O padrão estabelecido são *retry topics* (`deliveries.retry.1m`, `.5m`, `.15m`): um consumidor lê o registro, verifica se já venceu e, se não, **pausa a partição** até vencer. Funciona e é bem documentado na indústria, mas é mais código que a escada — gestão de pause/resume em vez de delegar o agendamento ao broker. Ironia registrada: a decisão mais distintiva do projeto é a primeira a morrer na migração.

2. **O modelo de confirmação é incompatível.** Rabbit tem ack por mensagem em ordem arbitrária, e prefetch como limite de entregas em voo. Kafka tem commit de offset por partição, ordenado — não existe "confirmar a 5 enquanto a 3 está em voo" sem ou perder a 3 ou bloquear. Concorrência dentro de uma partição exige rastrear offsets à mão, comitando o menor ainda não concluído. É bookkeeping que hoje o broker faz de graça.

   Consequência de projeto: "todo worker consome todos os shards" (§4.19) deixa de ser possível — no Kafka cada partição pertence a exatamente um consumidor do grupo. O raio de dano continua parecido, mas o mecanismo muda de prefetch por fila para atribuição de partição, com rebalanceamento ao escalar.

**Melhora na troca:** a DLQ vira um tópico comum e o replay sai de graça, porque o Kafka retém registros após o consumo — rebobinar offset é nativo. Retenção e auditoria ficam melhores do que no Rabbit.

**Custo estimado:** para quem já conhece Kafka, na ordem de uma a duas semanas de trabalho focado — o rastreio de offsets com pool concorrente e os retry topics são as partes delicadas; adapter de publish e DLQ são triviais; os testes de integração trocam de container e pouco mais.

**Como baratear desde já:** manter o broker atrás de uma porta estreita — `Enqueue`, `ScheduleRetry(delay)`, `DeadLetter` — e um consumidor por callback. Isso confina a maior parte da troca a um pacote. Honestamente, não confina tudo: a diferença de ack vaza no desenho de concorrência do worker, e nenhuma interface esconde isso.

**Quando isso se justifica:** RabbitMQ sustenta dezenas de milhares de mensagens/s em hardware decente, e no caso de uso de webhook o fanout HTTP satura muito antes do broker. O motivo realista para migrar não é throughput — é querer retenção e replay nativos. O que significa que, para este sistema, provavelmente nunca.

O degrau 2 tem uma verruga que vale registrar: com fila FIFO por shard, uma mensagem cujo tenant já está no teto precisa voltar para a fila, e isso gera rodagem em vazio quando um tenant domina o shard. Funciona bem em shard diverso, mal em shard concentrado — e é justamente por isso que sistemas muito grandes acabam em fila por tenant.

**O que não está no mapa, de propósito.** Cargas do tipo "5.000 projetos a 50.000 eventos/s cada" (250M/s) não são dimensionáveis a partir daqui: exigiriam ~834.000 conexões HTTP simultâneas só no primeiro cenário e centenas de máquinas apenas para sustentar socket. E o limite real seria do outro lado — 50.000 requisições/s sustentadas contra o servidor de um cliente. Quem absorve isso não usa webhook, usa streaming. Pré-construir para esse número sem dados de uso seria dimensionar no escuro.

---

## 6. O que ficou deliberadamente de fora

Registrado porque escopo negado é decisão:

| Fora | Por quê |
|---|---|
| Login e signup | Plugável depois via provider; caro agora e longe do valor do sistema |
| Billing, checkout, planos pagos | Plugável depois; a medição de uso, que é a parte cara, já existe |
| OpenTelemetry / tracing distribuído | Risco de virar o projeto |
| Criptografia de payload ponta a ponta | Mata adoção; enquadrada como feature de plano pago |
| Rotação de secret | Formato de assinatura já a suporta; o mecanismo é roadmap |
| Event types e filtro de subscription | Fanout para todos os endpoints da application é suficiente na v1 |
| Reativação automática de endpoint | Circuit breaker abre automático, fecha manual |
| mTLS | Gestão de certificado por endpoint, e indemonstrável numa demo pública |
| Fila dedicada por application | Isolamento verdadeiro, mas ciclo de vida de fila dinâmica não se paga sem tenant grande; ver degrau 3 |
| Cap de concorrência por application | Mecanismo conhecido (§5, degrau 2), sem retorno antes de haver tenant que monopolize shard |
| Número de shards proporcional à base | Rehashing exige drenar e migrar; fila vazia é grátis, então superprovisionar é estritamente melhor (§4.19) |
| Ordem garantida de entrega | Incompatível com retry: manter ordem faria um evento em backoff de 15min segurar todos os seguintes do projeto. Stripe e GitHub também não garantem |
| Ingress e management em processos separados | A separação que importa é de auth e contrato, e ela existe; dividir só se paga quando os perfis de tráfego divergirem |
| Particionamento de `delivery_attempts` | `DROP PARTITION` só vence `DELETE` em lote quando a tabela é grande; ver §4.27 |
| Secret revelável uma única vez | Sem rotação, perder o secret obrigaria a recriar o endpoint; entra junto com a rotação |

---

## 7. Telas do dashboard

1. **Endpoints** — URL, status, taxa de sucesso em 24h, última entrega. Criar, editar, desativar, reativar, revelar secret.
2. **Feed de entregas** — timestamp, `event_type`, endpoint, status, código HTTP, tempo de resposta, número de tentativas. Filtros por status e endpoint, atualizando por SSE.
3. **Detalhe da entrega** — payload enviado, headers de assinatura, e a timeline de tentativas com código, snippet da resposta, erro e "próxima tentativa às 14h32". Botão de replay quando `dead`.
4. **Playground** — dispara um evento de teste contra o `sink` escolhendo `/ok`, `/500` ou `/timeout`. É esta tela que permite exercitar o ciclo completo de falha, backoff e DLQ sem depender de um endpoint externo.
