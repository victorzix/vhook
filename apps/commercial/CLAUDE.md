# Comercial — deltas

Aplicam-se sobre [`../CLAUDE.md`](../CLAUDE.md). Aqui vai só o que difere.

Esta é a página de vendas do vhook. O leitor decide em segundos se continua, então **peso e indexação são requisitos, não otimização**.

## O que muda

| | Dashboard | Comercial |
|---|---|---|
| Animação | framer-motion | **anime.js**, e só ele |
| Estado de servidor | TanStack Query | **nenhum** |
| Estado de cliente | zustand | **nenhum** |
| Renderização | client-heavy | **estático / SSG** |

**anime.js é exclusivo daqui, framer-motion é proibido aqui.** A fronteira é por aplicação e não por tipo de animação, justamente para que nenhum bundle carregue as duas bibliotecas.

**Sem TanStack Query e sem zustand.** Se aparecer necessidade de estado de servidor nesta aplicação, o escopo saiu do lugar — página de vendas não consulta API. A exceção plausível é um formulário de contato, e aí é um `action` de server component, não um cliente HTTP.

## Consumo do `packages/ui`

Importe **componente por componente**. Trazer o kit inteiro anula o motivo de o comercial ser uma aplicação separada.

Se um componente do `packages/ui` arrasta dependência que só o dashboard precisa, ele não deveria estar compartilhado — duplique a versão simples aqui.

## Indexação

Metadata completa por rota e por locale, incluindo Open Graph. `generateMetadata` com o locale ativo, e `hreflang` entre as quatro versões — sem isso as traduções competem entre si no índice em vez de se reforçarem.

Imagem sempre por `next/image`, com dimensão declarada.

## i18n

Mesmos quatro locales: `pt-BR`, `en`, `es`, `fr`, e a mesma regra de chave nova entrar nos quatro no mesmo commit.

Aqui isso pesa mais que no dashboard por dois motivos: texto de venda **é** o conteúdo, não decoração; e uma rota com tradução faltando é uma rota que o buscador indexa pela metade — o `hreflang` aponta para uma versão que não existe de verdade.
