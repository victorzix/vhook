# Templates

Estrutura, domínios e índice em [`../README.md`](../README.md). Aqui ficam só os templates e como preenchê-los.

## Uma spec é uma pasta

```
docs/specs/delivery/005-retry-ladder/
├── spec.md      o quê, e como se prova
├── plan.md      o como, em passos de 2-5 min
└── result.md    onde a realidade divergiu, e a evidência
```

Os três arquivos são lidos juntos, então moram juntos.

| Template | Vira | Quando |
|---|---|---|
| `spec.md` | `<domínio>/NNN-name/spec.md` | Depois da entrevista, antes de qualquer código |
| `plan.md` | `<domínio>/NNN-name/plan.md` | Depois da spec aprovada |
| `result.md` | `<domínio>/NNN-name/result.md` | Ao encerrar a implementação |
| `architecture-decision.md` | Nova seção em `docs/ARCHITECTURE.md` | Quando a spec exige decisão que vale para todo o sistema |

## O fluxo

```
entrevista  →  spec  →  contratos  →  aprovação
     →  plan  →  TDD task por task  →  result  →  release
```

**A entrevista vem primeiro, sempre.** Invoque a skill `brainstorming`: uma pergunta por vez, alternativas com tradeoff, design apresentado e aprovado. A spec é o resultado escrito dessa conversa. Sem ela, a spec documenta a primeira ideia que apareceu — e a seção "Não entra" fica sem motivo.

**Os contratos entram entre a spec e a aprovação**, não depois. Editar `contracts/openapi.yaml` e `contracts/events/` faz parte de aprovar a spec — é ali que request, response e payload deixam de ser prosa e viram definição executável. Ver [`contracts/README.md`](../../../contracts/README.md).

Cada spec deve produzir software funcionando e demonstrável por si só, e virar uma release (`v0.1.0`, `v0.2.0`, …). Se uma spec não consegue ser demonstrada sozinha, ela está grande demais — quebre.

## Regra que vale para todos

**Nenhum placeholder.** `TBD`, `a definir`, `tratar erros adequadamente`, `validar corretamente` são falhas de documento, não rascunho. Se você não sabe o valor exato, a spec não está pronta — e implementar em cima dela produz a decisão por omissão, tomada por quem estiver com menos contexto.

Seção que não se aplica: escreva `Não se aplica` e o motivo em uma linha. Nunca deixe vazia — seção vazia num documento ensina que pode ficar vazia no próximo.
