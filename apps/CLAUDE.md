# Front — regras comuns

Valem para `apps/dashboard` e `apps/commercial`. Deltas do comercial em [`commercial/CLAUDE.md`](commercial/CLAUDE.md).

## Stack fechada

Next.js (App Router) · TypeScript · Tailwind · **shadcn/ui como base de todo componente** · react-hook-form + zod · TanStack Query · zustand · lucide-react · framer-motion · next-intl · Vitest + Testing Library + Playwright.

Workspace pnpm: `apps/*` consomem `packages/ui`.

`next-intl` foi escolha minha, não sua — é o padrão para App Router e o único que resolve locale no server component sem gambiarra. Trocar é barato agora, caro depois.

## Divisão de estado

Onde cada tipo de estado mora. Confundir isso é o bug mais comum desta stack:

| Estado | Dono |
|---|---|
| Vem da API | **TanStack Query**, e só ele |
| UI, preferência, seleção | **zustand** |
| Navegável e compartilhável (filtro, página) | **URL** |
| Formulário | **react-hook-form** |

**Nunca copiar dado do Query para o zustand.** No instante em que você faz isso existem duas verdades, e a do zustand não sabe que ficou velha. Se um componente precisa de dado do servidor, ele usa o hook do Query — mesmo que o pai já tenha buscado, porque o cache resolve.

## Prop drilling

Máximo **dois níveis**. Acima disso: composição (passar JSX em vez de dados), context da feature, ou zustand se for realmente global.

Prop que um componente só recebe para repassar adiante é o sinal — ela não pertence à interface dele.

## Onde as coisas moram

```
apps/dashboard/src/
├── app/<rota>/
│   ├── page.tsx          SÓ composição — nenhum componente definido aqui
│   └── _components/      componentes usados apenas nesta rota
├── features/<domínio>/
│   ├── hooks/            lógica com estado
│   ├── lib/              lógica pura, testável sem React
│   ├── schemas/          zod
│   └── api/              chamadas ao route handler do BFF
├── components/shared/    reutilizáveis dentro deste app
└── lib/                  utilitários genéricos
packages/ui/              shadcn + primitivos compartilhados entre os apps
```

**A escada de promoção de componente**, e ela só sobe:

1. usado numa rota só → `app/<rota>/_components/`
2. usado em duas rotas → `components/shared/`
3. usado nos dois apps → `packages/ui/`

Não criar em `shared/` "porque vai ser reutilizado". Promove quando o segundo uso aparecer — antes disso é adivinhação, e componente compartilhado prematuro nasce com abstração errada.

## Componentes

- **Página não define componente.** `page.tsx` compõe e nada mais.
- **Lógica complexa não vive em componente.** O componente recebe dados e callbacks; ele não decide. Se aparecer um `useEffect` com regra de negócio dentro, a regra vira hook em `features/<domínio>/hooks/`.
- **Lógica pura vai para `features/<domínio>/lib/`** — sem React, sem hook, testável em milissegundos. É onde o TDD roda rápido, do mesmo jeito que `internal/core` no back.
- **Schema zod nunca dentro de arquivo de componente.** Sempre em `features/<domínio>/schemas/`, importado. Schema é contrato, componente é apresentação.

## Props

- Nomear pelo **domínio**, não pelo layout: `endpoint`, `deliveryStatus` — nunca `data`, `item`, `value`.
- Booleano com prefixo: `isLoading`, `hasError`, `canReplay`.
- Callback nomeia o evento de domínio: `onReplayRequested`, não `onClick`.
- Componente com mais de ~7 props geralmente está fazendo duas coisas.

## Tipos da API

**Os tipos de request e response são gerados** de `contracts/openapi.yaml` via `openapi-typescript`. Nunca escritos à mão, nunca editados.

Zod serve para validar **entrada do usuário**, não para redeclarar o contrato. O submit é tipado com o tipo gerado, e aí o TypeScript acusa se o schema divergir do que a API aceita.

## Erros

A API devolve **só código**, nunca mensagem. O front traduz pelo catálogo `i18n/errors.<locale>.json` — mesmo arquivo que o Go usa, para não existirem dois textos para o mesmo código.

`details[]` aponta o campo: use para marcar o erro no campo do formulário, não para montar uma frase.

Erro sem tradução no catálogo é bug — há teste que falha se um código ficar sem entrada.

## Estilo e responsividade

- **Mobile first, sem exceção.** O estilo base é o de tela pequena; breakpoints só somam. `max-width` como padrão é o caminho errado.
- shadcn é a base de todo componente. Adaptar significa **copiar e ajustar** o componente, nunca sobrescrever de fora com `!important` ou seletor descendente.
- Ícones só de `lucide-react`.
- `framer-motion` para animação de interface. `anime.js` **não entra aqui** — é exclusivo do comercial.

## i18n

Quatro locales: `pt-BR`, `en`, `es`, `fr`.

**String literal visível em JSX é erro.** Todo texto passa pelo i18n, inclusive `aria-label`, `placeholder`, `title` e mensagem de validação — inclusive quando "é só um rótulo".

**Chave nova entra nos quatro locales no mesmo commit. Sem exceção.** Não existe "depois eu traduzo": a tradução que fica pra depois nunca acontece, e o resultado é interface bilíngue por acidente, com o usuário francês lendo metade em português.

Duas consequências dessa regra, e as duas são deliberadas:

- **Sem fallback de locale em runtime.** Cair para `en` quando falta `fr` parece robustez, mas é o que esconde a lacuna — a tela funciona, ninguém reclama, e a chave fica faltando por meses. Chave ausente tem que doer na hora.
- **Um teste falha se qualquer chave existir em um locale e não em outro.** É a única forma de "sempre, sem exceção" ser verdade em vez de intenção. Mesmo formato do teste de completude do catálogo de erros, que já segue essa regra.

Tradução provisória entra igual — provisória e presente é melhor que ausente. Revisar depois é trabalho; ter a chave faltando é bug.

## Testes

| Nível | Ferramenta | Onde |
|---|---|---|
| Lógica pura | Vitest | `features/*/lib` — é aqui que o ciclo red-green vive |
| Componente | Testing Library | comportamento visível, nunca estado interno |
| Fluxo | Playwright | só o caminho crítico |

TDD vale igual ao back: invoque a skill `test-driven-development` antes de escrever componente ou hook.

O fluxo que o Playwright precisa cobrir é o que só existe ao vivo: **uma entrega mudando de estado sozinha via SSE**, de `pending` a `failed` a `succeeded`.

## O browser nunca fala com a API Go

Toda chamada passa por route handler do Next. O token administrativo fica server-side, CORS deixa de existir, e quando o provider de auth entrar a troca acontece nessa camada. Chamar a API Go direto do cliente quebra as três coisas de uma vez.
