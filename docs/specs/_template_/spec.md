# NNN — <Nome da feature>

| | |
|---|---|
| **Status** | rascunho · em revisão · aprovada · implementada |
| **Release alvo** | `v0.X.0` |
| **Plano** | [`plan.md`](plan.md) |

---

## Problema

*Uma ou duas frases: o que não existe ou não funciona hoje. Sem solução aqui.*

> Exemplo: uma entrega que falha por 5xx é descartada, então uma indisponibilidade de 30 segundos no cliente perde o evento para sempre.

## Escopo

**Entra**

- *Item concreto e verificável.*

**Não entra**

- *Item + o motivo em uma linha.*

> O motivo importa mais que o item. Sem ele, a próxima pessoa (ou você em duas semanas) reabre a discussão do zero ou implementa por conta.

## Comportamento observável

*Contratos exatos. Request e response com corpo real, o que muda no banco, o que aparece no dashboard.*

**Proibido nesta seção:** "deve validar corretamente", "trata o erro apropriadamente", "retorna erro adequado". Valor exato, código HTTP exato, nome de coluna exato — ou a spec não está pronta.

```
POST /v1/exemplo
{ "campo": "valor" }
→ 202 { "id": "evt_01H..." }
```

| Situação | Resultado |
|---|---|
| *entrada válida* | *o que acontece, observável de fora* |
| *entrada inválida X* | *código + corpo exatos* |

## Modelo de dados

*Tabelas e colunas criadas ou alteradas. Migration necessária? Índice novo? Índice novo em tabela grande exige `CREATE INDEX CONCURRENTLY`.*

Não se aplica se a feature não toca o schema — diga isso explicitamente.

## Invariantes tocados

*Quais invariantes do [`CLAUDE.md`](../../CLAUDE.md) esta feature encosta, e como cada um continua valendo.*

> Exemplo: toca a ordem de ack. O publish na fila de espera acontece antes do ack da mensagem original, e o teste de integração cobre a morte do worker entre os dois.

Se não toca nenhum, diga. Se toca algum e você não sabe como preservá-lo, a spec não está pronta.

## Modos de falha

*A seção mais importante num sistema de entrega. Cada falha plausível e o comportamento esperado — porque a maior parte deste sistema **é** tratamento de falha, e o que não estiver aqui vai ser decidido no meio da implementação por quem tiver menos contexto.*

| Falha | Comportamento esperado | Observável onde |
|---|---|---|
| *cliente responde 503* | *retenta no nível N da escada* | *log com correlation ID, `vhook_deliveries_total{status="failed"}`* |
| *Postgres indisponível* | | |
| *worker morre no meio* | | |

## Como se prova que funciona

**Unidade** — `internal/core`, sem container. *Quais funções puras e quais casos.*

**Integração** — testcontainers. *Só o que exige Postgres ou RabbitMQ de verdade. Se um teste aqui exercita regra de negócio, a lógica está no pacote errado.*

**Manual** — *como exercitar pelo `sink` ou pelo playground, e o que observar em log, métrica e dashboard. Se não há forma de ver a feature funcionando de fora, não está pronta para demo — e demo é requisito deste projeto.*

## Decisões arquiteturais geradas

*Se apareceu decisão que vale para todo o sistema, registre em [`ARCHITECTURE.md`](../ARCHITECTURE.md) usando `_template_/architecture-decision.md` e linke aqui. Decisão global escondida dentro de uma spec de feature é decisão que ninguém vai achar.*

## Empurrado para o roadmap

*O que saiu do escopo e merece existir depois. Vai para a seção de roadmap do `README.md`.*
