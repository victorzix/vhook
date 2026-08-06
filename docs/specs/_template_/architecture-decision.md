# Template de decisão arquitetural

Cola como nova subseção numerada em [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md), seguindo a numeração existente (`§4.29`, `§4.30`, …).

Use isto **só para decisão que vale para todo o sistema**. Escolha local de uma feature mora na spec dela.

---

```markdown
### 4.NN <Título afirmativo — a decisão, não o tema>

**Decisão.** <O que foi escolhido, em uma ou duas frases. Concreto o bastante para alguém implementar sem perguntar.>

**Por quê.** <O raciocínio. Se houver um detalhe não-óbvio que faz a coisa funcionar, ele vive aqui.>

**Tradeoff.** <O que se abriu mão. Obrigatório.>

**Descartado.** <Alternativas consideradas e o motivo de cada uma cair. Obrigatório quando havia alternativa real.>
```

---

## As duas seções que ninguém quer escrever são as que importam

**`Tradeoff` é obrigatório.** Decisão apresentada só com vantagens lê como marketing e faz o leitor desconfiar do documento inteiro. E na prática é a seção mais útil no futuro: quando o tradeoff começar a doer, você quer descobrir que ele foi escolhido de olhos abertos, não que ninguém pensou nisso.

**`Descartado` é o que separa decisão de default.** Sem ela, quem lê não sabe se você avaliou as opções ou pegou a primeira. E é o que impede a mesma discussão de ser reaberta a cada dois meses.

Se você não consegue nomear um tradeoff nem uma alternativa descartada, provavelmente isso não é uma decisão arquitetural — é uma consequência de outra decisão. Registre onde ela pertence.

## Se uma decisão for revertida

Não apague. Marque a original e escreva a nova:

```markdown
### 4.NN <Título> — **revertida em §4.MM**
```

O caminho errado percorrido é informação. Para quem lê o repo, uma decisão revertida com o motivo vale mais que um documento que finge que sempre esteve certo.
