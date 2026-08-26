# Specs

## Estrutura

Agrupadas por domínio, numeradas globalmente:

```
docs/specs/
├── ingress/
│   ├── 001-receive-event/
│   └── 004-idempotency/
├── endpoints/
│   ├── 002-crud/
│   └── 006-circuit-breaker/
└── delivery/
    ├── 003-dispatch-with-timeout/
    └── 005-retry-ladder/
```

Cada pasta de spec tem os três arquivos: `spec.md`, `plan.md`, `result.md`.

**O domínio agrupa, o número ordena.** As duas informações são diferentes e as duas importam: a pasta responde "o que já existe sobre endpoints?", e o número responde "em que ordem isso foi construído?".

**A numeração é global e nunca reiniciada por domínio.** `spec 006` identifica uma spec sem ambiguidade, em qualquer conversa ou commit. Se cada domínio tivesse o seu `001`, toda referência precisaria carregar o domínio junto para não ser ambígua.

Os buracos na sequência dentro de uma pasta (`002`, `006`, `011`) não são defeito — são o registro de que aquele domínio evoluiu em momentos diferentes.

## Domínios

Espelham os pacotes, para que a spec e o código que ela produz fiquem próximos:

| Domínio | Cobre |
|---|---|
| `ingress` | recebimento, validação, idempotência, rate limit |
| `endpoints` | cadastro, ciclo de vida, secret, circuit breaker |
| `delivery` | disparo, assinatura, retry, DLQ, replay |
| `queue` | topologia, shards, publish e consumo |
| `dashboard` | telas e BFF |
| `platform` | compose, migrations, observabilidade, release |

Domínio novo se cria quando aparece a segunda spec que não cabe em nenhum existente. Uma spec sozinha não justifica pasta nova.

## Índice

| # | Domínio | Spec | Release | Status |
|---|---|---|---|---|
| 001 | `platform` | [Walking skeleton](platform/001-walking-skeleton/spec.md) | `v0.1.0` | implementada |

Uma spec só está aprovada quando entra nesta tabela. É a única visão que mostra ordem, release e status juntos — as pastas mostram agrupamento, não progresso.

## Antes de escrever qualquer spec: entrevista

**Invoque a skill `brainstorming`.** Ela conduz a entrevista: uma pergunta por vez, propõe alternativas com tradeoff, apresenta o design e pede aprovação. A spec é o resultado escrito dessa conversa, nunca o ponto de partida.

Spec escrita sem entrevista documenta a primeira solução que passou pela cabeça de alguém. A entrevista é onde as alternativas aparecem — e é o que faz a seção "Não entra" da spec ter motivo em vez de silêncio.

Vale mesmo para spec pequena. Se ela é realmente simples, a entrevista tem duas perguntas e termina rápido; o custo é baixo e a chance de descobrir que não era simples é justamente o motivo de fazer.

## O fluxo completo

```
entrevista  →  spec.md  →  contratos  →  aprovação
     →  plan.md  →  TDD task por task  →  result.md  →  release
```

Templates e regras de preenchimento em [`_template_/`](_template_/README.md).
