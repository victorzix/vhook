# Diagramas

Mermaid em markdown, renderizado nativamente pelo GitHub. Sem build, sem imagem para regenerar, sem ferramenta externa.

| Arquivo | Mostra |
|---|---|
| [`data-model.md`](data-model.md) | Modelo de dados — ER |
| [`delivery-flow.md`](delivery-flow.md) | Vida de uma entrega, do ingress à DLQ |

Novos diagramas entram com a spec que os torna necessários. Diagrama criado "porque é bom ter" é diagrama que ninguém atualiza.

## Por que Mermaid no repositório e não imagem

Imagem exportada de ferramenta de desenho tem duas fontes: o arquivo editável, que fica no computador de alguém, e o PNG, que fica no repo. Quando o schema muda, o PNG continua ali, plausível e errado.

Mermaid é texto. Entra no mesmo diff da migration, aparece em review, e quem alterar a tabela sem tocar no diagrama vai ver a inconsistência no próprio PR.

## A regra que impede duplicação

**Um conceito, um diagrama.** A topologia de filas **não** está aqui: ela vive como ASCII em [`ARCHITECTURE.md` §4.2](../ARCHITECTURE.md), junto da explicação que lhe dá sentido. Redesenhá-la em Mermaid criaria duas versões que divergem no primeiro ajuste da escada de retry.

Antes de adicionar diagrama, procure se o conceito já está desenhado em algum lugar. Se estiver, melhore o que existe ou mova — não duplique.

## Manutenção

Um diagrama errado é pior que diagrama nenhum, porque ele é acreditado. Se você mudou o schema ou o fluxo e não sabe como refletir aqui, prefira apagar o trecho obsoleto a deixá-lo desatualizado.
