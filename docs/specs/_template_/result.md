# NNN — <Nome da feature> · Resultado

| | |
|---|---|
| **Release** | `v0.X.0` |
| **Spec** | [`spec.md`](spec.md) |

---

## Este arquivo não repete o CHANGELOG

O CHANGELOG registra **o que foi entregue**. Aqui vai o que ele não consegue registrar: **onde a realidade divergiu da spec, e por quê**. Se você está escrevendo "implementei o que estava escrito", apague e escreva só `Sem divergências.` — é uma informação legítima e leva uma linha.

## Divergências da spec

*Cada item: o que a spec dizia, o que foi feito, e o motivo. Sem o motivo, isso vira uma lista de quebras de promessa em vez de aprendizado.*

| Spec dizia | Ficou | Por quê |
|---|---|---|
| | | |

Se alguma divergência muda o comportamento descrito na spec, **atualize a spec também** — ela é documento vivo, não ata de reunião. O histórico do git guarda o que ela dizia antes.

## Decisões que só apareceram implementando

*Escolhas que a spec não previu porque só ficam visíveis com o código na frente.*

Se alguma vale para todo o sistema, ela não pertence aqui: vai para `docs/ARCHITECTURE.md` pelo template de decisão, e este arquivo só linka.

## Evidência de que funciona

*Prova, não afirmação.*

- **Testes:** *quais rodaram e o resultado — cole a saída relevante, não escreva "todos passaram"*
- **Manual:** *o que foi exercitado pelo `sink` ou pelo playground, e o que foi observado*
- **Observabilidade:** *qual métrica ou log confirma o comportamento em execução*

## Contratos alterados

*O que mudou em `contracts/`. Se nada mudou, diga.*

## Pendente

*O que ficou de fora e para onde foi: roadmap do `README.md`, spec futura, ou dívida aceita e registrada.*

Item pendente sem destino é item esquecido.
