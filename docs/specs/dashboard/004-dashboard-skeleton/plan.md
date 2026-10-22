# Esqueleto do dashboard — Plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dar ao vhook um front que roda, com a tela de Endpoints funcionando contra um BFF mockado — e com o job de front do CI saindo do silêncio pela primeira vez.

**Architecture:** Workspace pnpm com um membro. O BFF são route handlers do Next que hoje devolvem fixture e amanhã fazem `fetch` na API Go — a costura fica onde vai ficar de verdade, então nada em `features/` precisa mudar depois. Os fixtures são tipados pelos tipos gerados de `contracts/openapi.yaml`, então não têm como divergir do contrato em silêncio.

**Tech Stack:** pnpm · Next.js App Router · TypeScript · Tailwind · shadcn/ui · TanStack Query · react-hook-form + zod · next-intl · Vitest + Testing Library · `openapi-typescript`

**Spec:** [`spec.md`](spec.md) · **Release alvo:** `v0.4.0`

## Global Constraints

Herdadas por toda task. Não repetir dentro delas.

- TDD estrito: nenhum código de produção sem teste que falhou primeiro. Ver skill `test-driven-development`.
- `internal/core` não importa Postgres, Rabbit nem `net/http`.
- Timeout de 5s por tentativa de entrega; `io.LimitReader` de 64KB na resposta.
- 4xx é falha permanente, exceto 408 e 429.
- Payload trafega como `[]byte` cru — nunca desserializar antes de assinar.
- Ack da mensagem original só depois do confirm do publish.
- DLQ por publicação explícita; nunca dead-letter na fila `deliveries`.
- Número de shards é constante em código, idêntica em `api`, `worker` e `reconciler`.
- Nenhuma métrica Prometheus com label `application_id`.
- Paginação por cursor keyset; nunca `OFFSET`.
- Env só para segredo e endereço; comportamento em código; por tenant no banco.
- Payload e headers de assinatura nunca em log.
- Documentação e commits em português; código, identificadores e logs em inglês.

**Do front, de [`apps/CLAUDE.md`](../../../../apps/CLAUDE.md):**

- **String literal visível em JSX é erro.** Todo texto passa pelo i18n, inclusive `aria-label`, `placeholder`, `title` e mensagem de validação.
- **Chave nova entra nos quatro locales no mesmo commit.** Sem fallback de locale em runtime — chave ausente tem que doer na hora.
- **Página não define componente.** `page.tsx` compõe e nada mais.
- **Nunca copiar dado do TanStack Query para o zustand.** Nesta release não há zustand.
- **Prop drilling máximo dois níveis.**
- **Props nomeadas pelo domínio**, nunca `data`, `item`, `value`. Booleano com prefixo `is`/`has`/`can`. Callback nomeia evento de domínio.
- **shadcn é a base de todo componente**, adaptado por cópia, nunca por `!important`.
- **Mobile first**, sem exceção.
- **Ícone só de `lucide-react`.**
- **Tipos de request e response são gerados**, nunca escritos à mão. Zod valida entrada do usuário, não redeclara o contrato.

**Específico desta release:**

- **`pnpm` não está instalado nesta máquina.** Node 22.13 e `npx` estão. O Step 1 da Task 1 resolve com `corepack`.
- **`make` não está instalado.** Rode o comando de dentro do alvo.
- **Só os route handlers em `app/api/` importam de `mocks/`.** Há teste de arquitetura na Task 7.
- **Quem commita é o dono do repositório.** Entregue a mensagem pronta; não rode `git commit`.

---

## Estrutura de arquivos

| Arquivo | Responsabilidade |
|---|---|
| `package.json` (raiz) | `packageManager`, para o corepack e o CI resolverem a mesma versão |
| `pnpm-workspace.yaml` | membros do workspace |
| `i18n/package.json` + `i18n/index.ts` | torna consumível pelo TS o catálogo que o Go já usa |
| `i18n/parity.test.ts` | o teste que `apps/CLAUDE.md` exige |
| `apps/dashboard/` | o app Next |
| `apps/dashboard/src/lib/api-types.ts` | **gerado** de `contracts/openapi.yaml` |
| `apps/dashboard/src/mocks/` | fixtures e estado em memória — **descartável** |
| `apps/dashboard/src/app/api/endpoints/` | BFF |
| `apps/dashboard/src/features/endpoints/lib/` | lógica pura — onde o red-green mora |
| `apps/dashboard/src/features/endpoints/schemas/` | zod |
| `apps/dashboard/src/features/endpoints/hooks/` | TanStack Query |
| `apps/dashboard/src/app/(dashboard)/endpoints/` | a tela |
| `Makefile` | `openapi-typescript` no alvo `generate` |

---

## Task 1: Workspace, app e o job de front saindo do silêncio

**Files:**
- Create: `package.json`, `pnpm-workspace.yaml`
- Create: `i18n/package.json`, `i18n/index.ts`, `i18n/parity.test.ts`, `i18n/vitest.config.ts`
- Create: `apps/dashboard/` (scaffold)
- Modify: `.gitignore`

**Interfaces:**
- Produces: `@vhook/i18n` com `errorCatalogs`, `locales`, `type Locale`; e os quatro scripts que o CI espera em cada pacote — `typecheck`, `lint`, `test`, `build`.

- [x] **Step 1: Habilitar o pnpm**

`pnpm` não está no PATH. Node 22 traz o `corepack`, que é a forma menos invasiva:

```bash
corepack enable pnpm
pnpm --version
```

Se `corepack` não estiver disponível, `npm i -g pnpm` resolve. **Reporte qual dos dois usou e a versão resultante** — ela vai para o `packageManager`.

- [x] **Step 2: Criar o workspace**

`pnpm-workspace.yaml`:

```yaml
packages:
  - "apps/*"
  - "i18n"
```

`package.json` na raiz. Substitua `<versão>` pela que o Step 1 reportou:

```json
{
  "name": "vhook",
  "private": true,
  "packageManager": "pnpm@<versão>"
}
```

O `packageManager` não é enfeite: o `pnpm/action-setup` do CI o lê para escolher a versão, então sem ele o CI e a sua máquina podem divergir.

- [x] **Step 3: Tornar o catálogo de erros consumível pelo TS**

`i18n/package.json`:

```json
{
  "name": "@vhook/i18n",
  "version": "0.0.0",
  "private": true,
  "main": "./index.ts",
  "types": "./index.ts",
  "scripts": {
    "test": "vitest run"
  },
  "devDependencies": {
    "vitest": "^3.0.0"
  }
}
```

`i18n/index.ts`:

```ts
// The error catalogue is shared between Go and TypeScript by design: go:embed
// on one side, this import on the other. One file, one text — see §4.29.
import en from "./errors.en.json";
import es from "./errors.es.json";
import fr from "./errors.fr.json";
import ptBR from "./errors.pt-BR.json";

export const errorCatalogs = {
  "pt-BR": ptBR,
  en,
  es,
  fr,
} as const;

export type Locale = keyof typeof errorCatalogs;

export const locales = Object.keys(errorCatalogs) as Locale[];

/** The locale of applications.locale by default, and the fallback nowhere. */
export const defaultLocale: Locale = "pt-BR";
```

`i18n/vitest.config.ts`:

```ts
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: { include: ["*.test.ts"] },
});
```

- [x] **Step 4: Escrever o teste de paridade, e vê-lo falhar**

Este é o teste que `apps/CLAUDE.md` exige. Escreva-o **antes** de conferir se os locales estão em paridade — se já estiverem, force o vermelho no Step 5.

`i18n/parity.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { errorCatalogs, locales } from "./index";

describe("catálogo de erros", () => {
  it("tem exatamente as mesmas chaves em todos os locales", () => {
    // Sem fallback em runtime é decisão de apps/CLAUDE.md: cair para `en`
    // quando falta `fr` esconde a lacuna, a tela funciona, e a chave fica
    // faltando por meses. Aqui a falta dói na hora.
    const reference = Object.keys(errorCatalogs["pt-BR"]).sort();

    for (const locale of locales) {
      const keys = Object.keys(errorCatalogs[locale]).sort();
      expect(keys, `locale ${locale} divergiu de pt-BR`).toEqual(reference);
    }
  });

  it("não tem mensagem vazia em nenhum locale", () => {
    for (const locale of locales) {
      for (const [code, message] of Object.entries(errorCatalogs[locale])) {
        expect(message.trim(), `${locale} / ${code}`).not.toBe("");
      }
    }
  });

  it("cobre o formato MOD-TYP-NNN em toda chave", () => {
    const format = /^[A-Z]{3}-[A-Z]{3}-\d{3}$/;
    for (const code of Object.keys(errorCatalogs["pt-BR"])) {
      expect(code, `chave fora do formato`).toMatch(format);
    }
  });
});
```

```bash
pnpm install
pnpm --filter @vhook/i18n test
```

- [x] **Step 5: Provar que o teste de paridade pega a falta**

Remova temporariamente uma chave de `i18n/errors.fr.json` e rode de novo.

Esperado: FAIL com `locale fr divergiu de pt-BR`. **Restaure a chave e rode até verde.**

Um teste de paridade que nunca foi visto vermelho não prova que pega a divergência — e este projeto já teve a versão Go dele pagar três vezes.

- [x] **Step 6: Criar o app**

```bash
pnpm create next-app@latest apps/dashboard \
  --typescript --tailwind --eslint --app --src-dir \
  --import-alias "@/*" --use-pnpm --no-turbopack
```

Depois acrescente as dependências da stack fechada de `apps/CLAUDE.md`:

```bash
pnpm --filter dashboard add @tanstack/react-query react-hook-form zod @hookform/resolvers next-intl lucide-react
pnpm --filter dashboard add -D vitest @vitejs/plugin-react jsdom @testing-library/react @testing-library/jest-dom @testing-library/user-event
```

`framer-motion` **não** entra: nenhuma animação nesta release, e biblioteca sem uso decide a forma antes de existir o problema.

- [x] **Step 7: Dar ao app os quatro scripts que o CI espera**

Em `apps/dashboard/package.json`, o bloco `scripts` precisa conter exatamente estes nomes — o CI roda `pnpm -r typecheck`, `lint`, `test` e `build`:

```json
{
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "next lint",
    "typecheck": "tsc --noEmit",
    "test": "vitest run"
  }
}
```

`apps/dashboard/vitest.config.ts`:

```ts
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
    globals: true,
  },
});
```

`apps/dashboard/vitest.setup.ts`:

```ts
import "@testing-library/jest-dom/vitest";
```

E em `next.config.ts`, compilar o pacote do workspace:

```ts
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // §4.35: sem depender de nada da Vercel. §3 fixa deploy em VPS com Coolify.
  output: "standalone",
  // @vhook/i18n é TypeScript cru no workspace; sem isto o Next não o compila.
  transpilePackages: ["@vhook/i18n"],
};

export default nextConfig;
```

Acrescente a dependência de workspace:

```bash
pnpm --filter dashboard add @vhook/i18n --workspace
```

- [x] **Step 8: Verificar que os quatro comandos do CI passam**

```bash
pnpm install --frozen-lockfile
pnpm -r typecheck
pnpm -r lint
pnpm -r test
pnpm -r build
```

Esperado: os quatro sem erro. O `test` roda os três casos de paridade — não é vacuoso.

Se `vitest run` reclamar de "no test files" em `apps/dashboard`, **não** acrescente `--passWithNoTests`: a Task 5 traz o primeiro teste do app, e um script que passa sem teste algum é um script que mente. Se precisar destravar o Step 8, rode `pnpm -r test --filter @vhook/i18n` e registre a pendência no relatório.

- [ ] **Step 9: Commit**

```bash
git add package.json pnpm-workspace.yaml pnpm-lock.yaml i18n apps/dashboard .gitignore
```

```
feat: criar o workspace pnpm e o app do dashboard
```

---

## Task 2: `openapi-typescript` no `make generate`

**Files:**
- Modify: `Makefile`
- Create: `apps/dashboard/src/lib/api-types.ts` (gerado)

**Interfaces:**
- Produces: `import type { components } from "@/lib/api-types"` — os tipos do contrato.

- [ ] **Step 1: Acrescentar a geração ao alvo**

A restrição que decide a forma: **`make generate` roda no job Go do CI**, que não faz `pnpm install`. Então a geração não pode depender de `node_modules`.

Em `Makefile`, no alvo `generate`, use `npx` com **versão fixada** — versão flutuante deixaria o `git diff --exit-code` do CI vermelho por motivo errado:

```make
generate:
	go tool sqlc generate
	go tool oapi-codegen -config contracts/oapi-codegen.yaml contracts/openapi.yaml
	npx --yes openapi-typescript@7.9.1 contracts/openapi.yaml -o apps/dashboard/src/lib/api-types.ts
```

Confirme que `7.9.1` existe e ajuste para a versão estável que o `npx` resolver. **Reporte a versão que ficou.**

- [ ] **Step 2: Gerar e ler o que saiu**

```bash
npx --yes openapi-typescript@<versão> contracts/openapi.yaml -o apps/dashboard/src/lib/api-types.ts
```

**Abra o arquivo** e anote os nomes reais. O `openapi-typescript` costuma produzir `components["schemas"]["Endpoint"]`, mas a forma exata é o que vale — as tasks seguintes dependem dela. Anote em especial:

- Como se referencia `Endpoint`, `EndpointWithSecret`, `EndpointList`, `CreateEndpointRequest`, `Error`, `ErrorBody`.
- Se os campos opcionais viraram `?` ou `| undefined`.

**Nunca edite o gerado.**

- [ ] **Step 3: Verificar que o typecheck aceita o gerado**

```bash
pnpm -r typecheck
```

Esperado: sem erro.

- [ ] **Step 4: Verificar que regerar é estável**

```bash
npx --yes openapi-typescript@<versão> contracts/openapi.yaml -o apps/dashboard/src/lib/api-types.ts
git diff --exit-code apps/dashboard/src/lib/api-types.ts
```

Esperado: sem diff. É o que faz a barreira do CI valer.

- [ ] **Step 5: Commit**

```bash
git add Makefile apps/dashboard/src/lib/api-types.ts
```

```
feat: gerar os tipos TS a partir do contrato
```

---

## Task 3: `next-intl` e o shell da aplicação

**Files:**
- Create: `apps/dashboard/src/i18n/request.ts`, `apps/dashboard/messages/{pt-BR,en,es,fr}.json`
- Modify: `apps/dashboard/src/app/layout.tsx`
- Create: `apps/dashboard/src/app/providers.tsx`

**Interfaces:**
- Consumes: `@vhook/i18n`
- Produces: `useTranslations` disponível na árvore; `QueryClientProvider` montado.

- [ ] **Step 1: Escrever as mensagens de interface nos quatro locales**

São **duas** famílias de texto, e confundi-las é o erro comum:

- `messages/<locale>.json` — texto de interface: rótulo, botão, título.
- `i18n/errors.<locale>.json` — catálogo de código de erro, **compartilhado com o Go**. Não duplique aqui.

`apps/dashboard/messages/pt-BR.json`:

```json
{
  "endpoints": {
    "title": "Endpoints",
    "empty": "Nenhum endpoint cadastrado ainda.",
    "urlLabel": "URL de destino",
    "urlPlaceholder": "https://api.suaempresa.com/webhooks",
    "create": "Cadastrar endpoint",
    "creating": "Cadastrando…",
    "secretLabel": "Secret de assinatura",
    "reveal": "Revelar secret",
    "hide": "Ocultar secret",
    "copy": "Copiar secret",
    "copied": "Copiado",
    "edit": "Corrigir URL",
    "save": "Salvar",
    "cancel": "Cancelar",
    "status": { "active": "Ativo", "disabled": "Desativado" },
    "createdAt": "Cadastrado em",
    "urlMustBeHttps": "O endereço precisa começar com https://",
    "unknownError": "Não foi possível concluir. Tente novamente."
  }
}
```

Traduza as **mesmas chaves** para `en.json`, `es.json` e `fr.json`. Chave nova nos quatro no mesmo commit — sem exceção, e sem fallback.

- [ ] **Step 2: Estender o teste de paridade para as mensagens de interface**

Em `apps/dashboard`, crie `src/i18n/messages.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import en from "../../messages/en.json";
import es from "../../messages/es.json";
import fr from "../../messages/fr.json";
import ptBR from "../../messages/pt-BR.json";

/** Achata { a: { b: "x" } } em ["a.b"], para comparar árvores de tradução. */
function flatten(value: unknown, prefix = ""): string[] {
  if (typeof value !== "object" || value === null) return [prefix];
  return Object.entries(value).flatMap(([key, child]) =>
    flatten(child, prefix ? `${prefix}.${key}` : key),
  );
}

describe("mensagens de interface", () => {
  it("têm exatamente as mesmas chaves em todos os locales", () => {
    const reference = flatten(ptBR).sort();
    for (const [name, messages] of Object.entries({ en, es, fr })) {
      expect(flatten(messages).sort(), `locale ${name} divergiu`).toEqual(reference);
    }
  });
});
```

```bash
pnpm --filter dashboard test
```

Esperado: PASS. Depois remova temporariamente uma chave de `fr.json`, rode de novo e **veja falhar**, então restaure. É o mesmo argumento do Step 5 da Task 1.

- [ ] **Step 3: Configurar o `next-intl`**

`apps/dashboard/src/i18n/request.ts`:

```ts
import { getRequestConfig } from "next-intl/server";

import { defaultLocale } from "@vhook/i18n";

// Locale fixo nesta release: não há sessão nem seletor. Quando o login existir,
// ele vem de lá — e é a única linha que muda.
export default getRequestConfig(async () => ({
  locale: defaultLocale,
  messages: (await import(`../../messages/${defaultLocale}.json`)).default,
}));
```

Siga a documentação do `next-intl` para o plugin em `next.config.ts`. **Confira a forma na versão instalada e reporte se divergir** — a API do `next-intl` mudou entre versões maiores.

- [ ] **Step 4: Montar os providers**

`apps/dashboard/src/app/providers.tsx`:

```tsx
"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

export function Providers({ children }: { children: ReactNode }) {
  // Criado no estado, não no módulo: um QueryClient de módulo é compartilhado
  // entre requisições no servidor, e aí o cache de um usuário vaza para outro.
  const [queryClient] = useState(() => new QueryClient());
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}
```

Envolva o `children` do `layout.tsx` com `Providers` e com o `NextIntlClientProvider`.

- [ ] **Step 5: Verificar**

```bash
pnpm -r typecheck && pnpm -r lint && pnpm -r test && pnpm -r build
```

Esperado: os quatro sem erro.

- [ ] **Step 6: Commit**

```bash
git add apps/dashboard
```

```
feat: montar i18n e providers do dashboard
```

---

## Task 4: Os mocks e o BFF

**Files:**
- Create: `apps/dashboard/src/mocks/README.md`, `fixtures/endpoints.ts`, `store.ts`
- Create: `apps/dashboard/src/app/api/endpoints/route.ts`, `apps/dashboard/src/app/api/endpoints/[id]/route.ts`

**Interfaces:**
- Consumes: `@/lib/api-types`
- Produces: as quatro rotas do BFF; `mockStore` com `list`, `create`, `get`, `updateUrl`.

- [ ] **Step 1: Escrever o README da remoção**

Escreva **primeiro**, enquanto o contexto está fresco. Instrução de remoção escrita depois nunca é escrita.

`apps/dashboard/src/mocks/README.md`:

```markdown
# Mocks — pasta descartável

O BFF desta release devolve fixture em vez de chamar a API Go. A costura está
nos route handlers, que é onde ela vai ficar de verdade: nada em `features/`
nem em `app/(dashboard)/` sabe que isto existe.

## Para conectar à API de verdade

1. Em `src/app/api/endpoints/route.ts` e `src/app/api/endpoints/[id]/route.ts`,
   troque as chamadas a `mockStore` por `fetch` contra a API Go, montando o
   caminho `/v1/applications/{VHOOK_APPLICATION_ID}/endpoints` e mandando
   `Authorization: Bearer ${VHOOK_ADMIN_TOKEN}`.
2. Acrescente `VHOOK_API_URL`, `VHOOK_ADMIN_TOKEN` e `VHOOK_APPLICATION_ID`
   ao ambiente do dashboard.
3. Apague esta pasta inteira.

Nada além dos dois route handlers importa daqui, e há teste de arquitetura
garantindo isso — se ele passar depois do passo 3, a remoção está completa.

## O gate que passa a ser obrigatório

Enquanto os dados são inventados, a tela aberta é inofensiva. No momento em que
o BFF falar com a API de verdade, quem alcançar a URL revela secret de
assinatura real. Basic auth no proxy do Coolify resolve — ver a spec 004.
```

- [ ] **Step 2: Escrever os fixtures, tipados pelo contrato**

`apps/dashboard/src/mocks/fixtures/endpoints.ts`:

```ts
import type { components } from "@/lib/api-types";

// Tipados pelo contrato de propósito: se contracts/openapi.yaml mudar, o build
// quebra AQUI. É o que separa "mock que reflete a API" de "mock que refletia
// a API há três semanas".
type EndpointWithSecret = components["schemas"]["EndpointWithSecret"];

export const seedEndpoints: EndpointWithSecret[] = [
  {
    id: "ept_01J4PMX3R0E008000000000003",
    url: "https://api.acme.com/webhooks/vhook",
    status: "active",
    created_at: "2026-08-20T14:03:11Z",
    secret: "whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H",
  },
];

/**
 * Hosts que o mock recusa, para os caminhos de erro serem exercitáveis à mão.
 * Sem isto o tratamento de erro é escrito e nunca visto funcionando.
 */
export const forbiddenHosts = ["10.0.0.1", "169.254.169.254", "localhost"];

/** §4.28: o plano free permite 2 endpoints por application. */
export const planLimit = 2;
```

Ajuste `components["schemas"][...]` ao que a Task 2 anotou.

- [ ] **Step 3: Escrever o estado em memória**

`apps/dashboard/src/mocks/store.ts`:

```ts
import type { components } from "@/lib/api-types";

import { forbiddenHosts, planLimit, seedEndpoints } from "./fixtures/endpoints";

type EndpointWithSecret = components["schemas"]["EndpointWithSecret"];

/** O código de erro que o BFF deve devolver, ou null quando pode prosseguir. */
export type MockRejection =
  | "EPT-VAL-001"
  | "EPT-VAL-002"
  | "EPT-CFL-001"
  | "RTL-LMT-001"
  | "EPT-NFD-001";

// Estado de módulo: reinicia com o servidor e é por instância. Irrelevante
// agora, e some junto com a pasta.
let endpoints: EndpointWithSecret[] = [...seedEndpoints];

function randomSecret(): string {
  const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
  let body = "";
  for (let i = 0; i < 43; i += 1) {
    body += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return `whsec_${body}`;
}

function randomId(): string {
  const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
  let body = "";
  for (let i = 0; i < 26; i += 1) {
    body += crockford[Math.floor(Math.random() * crockford.length)];
  }
  return `ept_${body}`;
}

function reject(url: string, ignoreId?: string): MockRejection | null {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return "EPT-VAL-001";
  }
  if (parsed.protocol !== "https:") return "EPT-VAL-001";
  if (forbiddenHosts.includes(parsed.hostname)) return "EPT-VAL-002";
  if (endpoints.some((e) => e.url === url && e.id !== ignoreId)) return "EPT-CFL-001";
  return null;
}

export const mockStore = {
  /** A listagem NUNCA carrega o secret — §4.28 e a spec 003. */
  list(): components["schemas"]["Endpoint"][] {
    return endpoints.map(({ secret: _secret, ...rest }) => rest);
  },

  get(id: string): EndpointWithSecret | MockRejection {
    return endpoints.find((e) => e.id === id) ?? "EPT-NFD-001";
  },

  create(url: string): EndpointWithSecret | MockRejection {
    if (endpoints.length >= planLimit) return "RTL-LMT-001";
    const rejection = reject(url);
    if (rejection) return rejection;

    const created: EndpointWithSecret = {
      id: randomId(),
      url,
      status: "active",
      created_at: new Date().toISOString(),
      secret: randomSecret(),
    };
    endpoints = [...endpoints, created];
    return created;
  },

  updateUrl(id: string, url: string): components["schemas"]["Endpoint"] | MockRejection {
    const existing = endpoints.find((e) => e.id === id);
    if (!existing) return "EPT-NFD-001";
    const rejection = reject(url, id);
    if (rejection) return rejection;

    // O secret sobrevive ao PATCH: é o motivo de o PATCH existir.
    const updated = { ...existing, url };
    endpoints = endpoints.map((e) => (e.id === id ? updated : e));
    const { secret: _secret, ...withoutSecret } = updated;
    return withoutSecret;
  },
};
```

- [ ] **Step 4: Escrever os route handlers**

`apps/dashboard/src/app/api/endpoints/route.ts`:

```ts
import { NextResponse } from "next/server";

import { mockStore, type MockRejection } from "@/mocks/store";

/** Status de cada código, espelhando o que a API Go devolve (spec 003). */
const statusFor: Record<MockRejection, number> = {
  "EPT-VAL-001": 422,
  "EPT-VAL-002": 422,
  "EPT-CFL-001": 409,
  "RTL-LMT-001": 403,
  "EPT-NFD-001": 404,
};

/**
 * O envelope de erro do projeto: código, correlation id e details. NUNCA
 * mensagem — o front traduz o código pelo catálogo (§4.29).
 */
export function errorEnvelope(code: MockRejection) {
  return NextResponse.json(
    {
      error: {
        code,
        correlation_id: crypto.randomUUID(),
        details: code.startsWith("EPT-VAL") ? [{ field: "url", code }] : undefined,
      },
    },
    { status: statusFor[code] },
  );
}

export async function GET() {
  return NextResponse.json({ endpoints: mockStore.list() });
}

export async function POST(request: Request) {
  const body = (await request.json()) as { url?: unknown };
  if (typeof body.url !== "string") return errorEnvelope("EPT-VAL-001");

  const result = mockStore.create(body.url);
  if (typeof result === "string") return errorEnvelope(result);
  return NextResponse.json(result, { status: 201 });
}
```

`apps/dashboard/src/app/api/endpoints/[id]/route.ts`:

```ts
import { NextResponse } from "next/server";

import { mockStore } from "@/mocks/store";

import { errorEnvelope } from "../route";

export async function GET(_request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const result = mockStore.get(id);
  if (typeof result === "string") return errorEnvelope(result);
  return NextResponse.json(result);
}

export async function PATCH(request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const body = (await request.json()) as { url?: unknown };
  if (typeof body.url !== "string") return errorEnvelope("EPT-VAL-001");

  const result = mockStore.updateUrl(id, body.url);
  if (typeof result === "string") return errorEnvelope(result);
  return NextResponse.json(result);
}
```

**A assinatura de `params` mudou entre versões maiores do Next** — em 15 é `Promise`. Confira na versão instalada e adapte, reportando.

- [ ] **Step 5: Verificar**

```bash
pnpm -r typecheck && pnpm -r lint && pnpm -r build
```

Esperado: sem erro. Depois `pnpm --filter dashboard dev` e:

```bash
curl -s localhost:3000/api/endpoints | head -5
curl -s -X POST localhost:3000/api/endpoints -H 'Content-Type: application/json' -d '{"url":"http://acme.com/x"}'
curl -s -X POST localhost:3000/api/endpoints -H 'Content-Type: application/json' -d '{"url":"https://10.0.0.1/x"}'
```

Esperado: lista com um endpoint **sem secret** · `EPT-VAL-001` com 422 · `EPT-VAL-002` com 422.

- [ ] **Step 6: Commit**

```bash
git add apps/dashboard/src/mocks apps/dashboard/src/app/api
```

```
feat: servir endpoints por bff mockado
```

---

## Task 5: A lógica pura

Onde o ciclo red-green vive, sem React e sem DOM. É o `internal/core` do front.

**Files:**
- Create: `apps/dashboard/src/features/endpoints/lib/secret.ts` + `.test.ts`
- Create: `apps/dashboard/src/features/endpoints/lib/errors.ts` + `.test.ts`
- Create: `apps/dashboard/src/features/endpoints/schemas/endpoint.ts` + `.test.ts`

**Interfaces:**
- Produces:
  - `maskSecret(secret: string): string`
  - `fieldForErrorCode(code: string): "url" | null`
  - `isPlanLimitError(code: string): boolean`
  - `messageForErrorCode(code: string, locale: Locale): string`
  - `createEndpointSchema` (zod)

- [ ] **Step 1: Escrever os testes do mascaramento**

`apps/dashboard/src/features/endpoints/lib/secret.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { maskSecret } from "./secret";

const real = "whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H";

describe("maskSecret", () => {
  it("mantém o prefixo, para o valor continuar reconhecível", () => {
    expect(maskSecret(real).startsWith("whsec_")).toBe(true);
  });

  it("mantém os últimos quatro caracteres, para conferência visual", () => {
    expect(maskSecret(real).endsWith("mD8H")).toBe(true);
  });

  it("esconde o miolo", () => {
    // O dashboard é mostrado para gente: secret em tela vaza em screenshot,
    // em compartilhamento de tela e em gravação.
    expect(maskSecret(real)).not.toContain("zDccFjpqVDQ");
  });

  it("não devolve string vazia para entrada válida", () => {
    expect(maskSecret(real).length).toBeGreaterThan(10);
  });

  it("não quebra com entrada curta ou vazia", () => {
    expect(() => maskSecret("")).not.toThrow();
    expect(() => maskSecret("whsec_ab")).not.toThrow();
    expect(maskSecret("whsec_ab")).not.toContain("ab");
  });
});
```

- [ ] **Step 2: Rodar para ver falhar**

```bash
pnpm --filter dashboard test
```

Esperado: FAIL, `Failed to resolve import "./secret"`.

- [ ] **Step 3: Implementar o mascaramento**

`apps/dashboard/src/features/endpoints/lib/secret.ts`:

```ts
const PREFIX = "whsec_";
const VISIBLE_TAIL = 4;

/**
 * Hides the middle of a signing secret. §4.28 allows revealing it as often as
 * you like, and the detail route returns it — but this dashboard gets shown to
 * people, and a secret on screen leaks into screenshots and screen shares.
 */
export function maskSecret(secret: string): string {
  const body = secret.startsWith(PREFIX) ? secret.slice(PREFIX.length) : secret;
  if (body.length <= VISIBLE_TAIL) {
    return `${PREFIX}${"•".repeat(8)}`;
  }
  return `${PREFIX}${"•".repeat(8)}${body.slice(-VISIBLE_TAIL)}`;
}
```

- [ ] **Step 4: Rodar para ver passar**

```bash
pnpm --filter dashboard test
```

Esperado: PASS nos cinco.

- [ ] **Step 5: Escrever os testes do mapeamento de erro**

`apps/dashboard/src/features/endpoints/lib/errors.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { fieldForErrorCode, isPlanLimitError, messageForErrorCode } from "./errors";

describe("fieldForErrorCode", () => {
  it("aponta a URL para os três erros de validação de destino", () => {
    // Os três foram cunhados separados na spec 003 justamente porque
    // produzem mensagens diferentes: um código só forçaria a tela a adivinhar.
    for (const code of ["EPT-VAL-001", "EPT-VAL-002", "EPT-VAL-003"]) {
      expect(fieldForErrorCode(code)).toBe("url");
    }
  });

  it("aponta a URL para a duplicidade", () => {
    expect(fieldForErrorCode("EPT-CFL-001")).toBe("url");
  });

  it("não aponta campo para o limite do plano", () => {
    expect(fieldForErrorCode("RTL-LMT-001")).toBeNull();
  });

  it("não aponta campo para código desconhecido", () => {
    expect(fieldForErrorCode("XXX-YYY-999")).toBeNull();
  });
});

describe("isPlanLimitError", () => {
  it("reconhece o limite do plano", () => {
    expect(isPlanLimitError("RTL-LMT-001")).toBe(true);
    expect(isPlanLimitError("EPT-VAL-001")).toBe(false);
  });
});

describe("messageForErrorCode", () => {
  it("traduz pelo catálogo compartilhado com o Go", () => {
    const message = messageForErrorCode("EPT-VAL-001", "pt-BR");
    expect(message).not.toBe("");
    expect(message).not.toBe("EPT-VAL-001");
  });

  it("traduz o mesmo código de forma diferente em locales diferentes", () => {
    // Se estes forem iguais, o catálogo não está sendo consultado de verdade.
    expect(messageForErrorCode("EPT-VAL-001", "pt-BR")).not.toBe(
      messageForErrorCode("EPT-VAL-001", "fr"),
    );
  });

  it("devolve string vazia para código sem entrada, em vez de inventar texto", () => {
    // Chave ausente tem que doer: apps/CLAUDE.md recusa fallback de locale.
    expect(messageForErrorCode("XXX-YYY-999", "pt-BR")).toBe("");
  });
});
```

- [ ] **Step 6: Rodar para ver falhar**

Esperado: FAIL, `Failed to resolve import "./errors"`.

- [ ] **Step 7: Implementar o mapeamento**

`apps/dashboard/src/features/endpoints/lib/errors.ts`:

```ts
import { errorCatalogs, type Locale } from "@vhook/i18n";

/** Codes whose failure belongs to the URL field of the form. */
const URL_FIELD_CODES = new Set([
  "EPT-VAL-001",
  "EPT-VAL-002",
  "EPT-VAL-003",
  "EPT-CFL-001",
]);

const PLAN_LIMIT_CODE = "RTL-LMT-001";

/** Which form field to mark, or null when the failure is not about a field. */
export function fieldForErrorCode(code: string): "url" | null {
  return URL_FIELD_CODES.has(code) ? "url" : null;
}

export function isPlanLimitError(code: string): boolean {
  return code === PLAN_LIMIT_CODE;
}

/**
 * Translates through the same catalogue the Go side embeds — one file, one
 * text. Returns "" for an unknown code instead of inventing one: a missing key
 * has to hurt, which is why there is no runtime locale fallback.
 */
export function messageForErrorCode(code: string, locale: Locale): string {
  const catalogue = errorCatalogs[locale] as Record<string, string>;
  return catalogue[code] ?? "";
}
```

- [ ] **Step 8: Rodar para ver passar**

Esperado: PASS nos oito.

- [ ] **Step 9: Escrever o schema zod e seus testes**

`apps/dashboard/src/features/endpoints/schemas/endpoint.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { createEndpointSchema } from "./endpoint";

describe("createEndpointSchema", () => {
  it("aceita https", () => {
    expect(createEndpointSchema.safeParse({ url: "https://acme.com/hooks" }).success).toBe(true);
  });

  it("recusa http antes de chegar ao BFF", () => {
    // Validar aqui é o que faz o erro aparecer sem ida ao servidor. A garantia
    // continua sendo do back — isto é conveniência, não segurança.
    expect(createEndpointSchema.safeParse({ url: "http://acme.com/hooks" }).success).toBe(false);
  });

  it("recusa texto que não é URL", () => {
    expect(createEndpointSchema.safeParse({ url: "não é uma url" }).success).toBe(false);
  });

  it("recusa vazio", () => {
    expect(createEndpointSchema.safeParse({ url: "" }).success).toBe(false);
  });
});
```

- [ ] **Step 10: Rodar para ver falhar, implementar, rodar para ver passar**

`apps/dashboard/src/features/endpoints/schemas/endpoint.ts`:

```ts
import { z } from "zod";

import type { components } from "@/lib/api-types";

/**
 * Validates user input, and does NOT redeclare the contract: the submit is
 * typed with the generated type below, so TypeScript complains if the two
 * drift apart.
 */
export const createEndpointSchema = z.object({
  url: z
    .string()
    .url()
    .refine((value) => value.startsWith("https://"), {
      // The key, not the sentence: the component translates it.
      message: "urlMustBeHttps",
    }),
});

export type CreateEndpointInput = z.infer<typeof createEndpointSchema>;

// Compile-time check that the form shape matches what the API accepts.
type ContractRequest = components["schemas"]["CreateEndpointRequest"];
const _shapeMatchesContract: ContractRequest = { url: "https://example.com" } satisfies CreateEndpointInput;
void _shapeMatchesContract;
```

- [ ] **Step 11: Commit**

```bash
git add apps/dashboard/src/features/endpoints
```

```
feat: mascarar secret e traduzir erro pelo catálogo
```

---

## Task 6: A tela de Endpoints

**Files:**
- Create: `apps/dashboard/src/features/endpoints/api/client.ts`
- Create: `apps/dashboard/src/features/endpoints/hooks/use-endpoints.ts`
- Create: `apps/dashboard/src/app/(dashboard)/endpoints/page.tsx`
- Create: `apps/dashboard/src/app/(dashboard)/endpoints/_components/` (lista, formulário, secret)
- Create: os testes de componente correspondentes

**Interfaces:**
- Consumes: tudo da Task 5, mais os route handlers da Task 4.

- [ ] **Step 1: Instalar os componentes shadcn que a tela usa**

```bash
pnpm --filter dashboard dlx shadcn@latest init
pnpm --filter dashboard dlx shadcn@latest add button input form card table badge
```

Eles são **copiados para dentro do app**, não importados de biblioteca. Ficam em `apps/dashboard/src/components/ui/` — e é por isso que `packages/ui` não precisa existir ainda: mover depois é `git mv`.

- [ ] **Step 2: Escrever o cliente do BFF**

`apps/dashboard/src/features/endpoints/api/client.ts`:

```ts
import type { components } from "@/lib/api-types";

export type Endpoint = components["schemas"]["Endpoint"];
export type EndpointWithSecret = components["schemas"]["EndpointWithSecret"];

/** The error the BFF returns: code and correlation id, never a message. */
export class ApiError extends Error {
  constructor(
    readonly code: string,
    readonly correlationId: string,
  ) {
    // The message is the code on purpose: any text here would be text the
    // catalogue is supposed to own.
    super(code);
  }
}

async function parse<T>(response: Response): Promise<T> {
  const body = await response.json();
  if (!response.ok) {
    throw new ApiError(body?.error?.code ?? "SYS-INT-001", body?.error?.correlation_id ?? "");
  }
  return body as T;
}

export async function listEndpoints(): Promise<Endpoint[]> {
  const body = await parse<{ endpoints: Endpoint[] }>(await fetch("/api/endpoints"));
  return body.endpoints;
}

export async function createEndpoint(url: string): Promise<EndpointWithSecret> {
  return parse<EndpointWithSecret>(
    await fetch("/api/endpoints", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url }),
    }),
  );
}

export async function updateEndpointUrl(id: string, url: string): Promise<Endpoint> {
  return parse<Endpoint>(
    await fetch(`/api/endpoints/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url }),
    }),
  );
}
```

- [ ] **Step 3: Escrever os hooks**

`apps/dashboard/src/features/endpoints/hooks/use-endpoints.ts`:

```ts
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { createEndpoint, listEndpoints, updateEndpointUrl } from "../api/client";

const KEY = ["endpoints"] as const;

export function useEndpoints() {
  return useQuery({ queryKey: KEY, queryFn: listEndpoints });
}

export function useCreateEndpoint() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: createEndpoint,
    // Invalidate instead of writing into the cache by hand: the server is the
    // one truth, and apps/CLAUDE.md forbids a second copy of it.
    onSuccess: () => client.invalidateQueries({ queryKey: KEY }),
  });
}

export function useUpdateEndpointUrl() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, url }: { id: string; url: string }) => updateEndpointUrl(id, url),
    onSuccess: () => client.invalidateQueries({ queryKey: KEY }),
  });
}
```

- [ ] **Step 4: Escrever o teste de componente do formulário**

`apps/dashboard/src/app/(dashboard)/endpoints/_components/endpoint-form.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it, vi } from "vitest";

import messages from "../../../../../messages/pt-BR.json";
import { EndpointForm } from "./endpoint-form";

function renderForm(props: Partial<Parameters<typeof EndpointForm>[0]> = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <NextIntlClientProvider locale="pt-BR" messages={messages}>
      <QueryClientProvider client={client}>
        <EndpointForm isAtPlanLimit={false} onEndpointCreated={vi.fn()} {...props} />
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

describe("EndpointForm", () => {
  it("recusa http antes de chamar o BFF", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderForm();

    await userEvent.type(screen.getByLabelText(messages.endpoints.urlLabel), "http://acme.com/x");
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.create }));

    expect(await screen.findByText(messages.endpoints.urlMustBeHttps)).toBeInTheDocument();
    // A validação local é o que evita a ida ao servidor. Se o fetch aconteceu,
    // ela não está no lugar certo.
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("desabilita o cadastro quando o plano está no limite", () => {
    renderForm({ isAtPlanLimit: true });
    expect(screen.getByRole("button", { name: messages.endpoints.create })).toBeDisabled();
  });
});
```

- [ ] **Step 5: Rodar para ver falhar**

Esperado: FAIL, `Failed to resolve import "./endpoint-form"`.

- [ ] **Step 6: Implementar o formulário**

`_components/endpoint-form.tsx`:

```tsx
"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useLocale, useTranslations } from "next-intl";
import { useForm } from "react-hook-form";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ApiError, type EndpointWithSecret } from "@/features/endpoints/api/client";
import { useCreateEndpoint } from "@/features/endpoints/hooks/use-endpoints";
import {
  fieldForErrorCode,
  isPlanLimitError,
  messageForErrorCode,
} from "@/features/endpoints/lib/errors";
import {
  createEndpointSchema,
  type CreateEndpointInput,
} from "@/features/endpoints/schemas/endpoint";
import type { Locale } from "@vhook/i18n";

type Props = {
  isAtPlanLimit: boolean;
  onEndpointCreated: (endpoint: EndpointWithSecret) => void;
};

export function EndpointForm({ isAtPlanLimit, onEndpointCreated }: Props) {
  const t = useTranslations("endpoints");
  const locale = useLocale() as Locale;
  const create = useCreateEndpoint();

  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors },
  } = useForm<CreateEndpointInput>({ resolver: zodResolver(createEndpointSchema) });

  async function onSubmit(input: CreateEndpointInput) {
    try {
      onEndpointCreated(await create.mutateAsync(input.url));
      reset();
    } catch (error) {
      if (!(error instanceof ApiError)) {
        setError("root", { message: t("unknownError") });
        return;
      }
      const field = fieldForErrorCode(error.code);
      // The catalogue owns the text; this component only decides where it goes.
      const message = messageForErrorCode(error.code, locale) || t("unknownError");
      setError(field ?? "root", { message });
    }
  }

  // The zod message is a KEY, not a sentence — translated here so the schema
  // stays free of copy.
  const urlError = errors.url?.message ? t(errors.url.message) : undefined;

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-3">
      <label htmlFor="url" className="text-sm font-medium">
        {t("urlLabel")}
      </label>
      <Input
        id="url"
        {...register("url")}
        placeholder={t("urlPlaceholder")}
        aria-invalid={Boolean(urlError)}
        aria-describedby={urlError ? "url-error" : undefined}
      />
      {urlError ? (
        <p id="url-error" role="alert" className="text-sm text-destructive">
          {urlError}
        </p>
      ) : null}
      {errors.root?.message ? (
        <p role="alert" className="text-sm text-destructive">
          {errors.root.message}
        </p>
      ) : null}
      <Button type="submit" disabled={isAtPlanLimit || create.isPending}>
        {create.isPending ? t("creating") : t("create")}
      </Button>
    </form>
  );
}
```

Repare em duas coisas que a regra do projeto exige e que é fácil errar: **`placeholder` e `aria-*` também passam pelo i18n**, e a mensagem do zod é uma **chave** traduzida aqui — schema é contrato, não lugar de texto.

O `isPlanLimitError` é usado pela página, não pelo formulário: é ela que sabe quantos endpoints existem. Ver o Step 9.

- [ ] **Step 7: Rodar para ver passar**

Esperado: PASS nos dois.

- [ ] **Step 8: Escrever o teste do secret revelável**

`_components/endpoint-secret.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it } from "vitest";

import messages from "../../../../../messages/pt-BR.json";
import { EndpointSecret } from "./endpoint-secret";

const secret = "whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H";

function renderSecret() {
  return render(
    <NextIntlClientProvider locale="pt-BR" messages={messages}>
      <EndpointSecret secret={secret} />
    </NextIntlClientProvider>,
  );
}

describe("EndpointSecret", () => {
  it("começa mascarado", () => {
    renderSecret();
    expect(screen.queryByText(secret)).not.toBeInTheDocument();
  });

  it("revela o valor completo ao pedir", async () => {
    renderSecret();
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.reveal }));
    expect(screen.getByText(secret)).toBeInTheDocument();
  });

  it("volta a esconder", async () => {
    renderSecret();
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.reveal }));
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.hide }));
    expect(screen.queryByText(secret)).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 9: Implementar `EndpointSecret`, e ver passar**

`_components/endpoint-secret.tsx`:

```tsx
"use client";

import { Eye, EyeOff } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { maskSecret } from "@/features/endpoints/lib/secret";

export function EndpointSecret({ secret }: { secret: string }) {
  // Masked by default: this dashboard gets shown to people, and a secret on
  // screen leaks into screenshots, screen shares and recordings.
  const [isRevealed, setIsRevealed] = useState(false);
  const t = useTranslations("endpoints");

  return (
    <div className="flex items-center gap-2">
      <code className="font-mono text-sm">{isRevealed ? secret : maskSecret(secret)}</code>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => setIsRevealed((current) => !current)}
      >
        {isRevealed ? <EyeOff aria-hidden /> : <Eye aria-hidden />}
        {isRevealed ? t("hide") : t("reveal")}
      </Button>
    </div>
  );
}
```

O ícone leva `aria-hidden` porque o texto do botão já nomeia a ação — sem isso o leitor de tela anuncia duas vezes.

- [ ] **Step 10: Escrever o teste da listagem, e vê-lo falhar**

`_components/endpoint-list.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it } from "vitest";

import messages from "../../../../../messages/pt-BR.json";
import { EndpointList } from "./endpoint-list";

const endpoints = [
  {
    id: "ept_01J4PMX3R0E008000000000003",
    url: "https://api.acme.com/webhooks",
    status: "active" as const,
    created_at: "2026-08-20T14:03:11Z",
  },
];

function renderList(items = endpoints) {
  return render(
    <NextIntlClientProvider locale="pt-BR" messages={messages}>
      <EndpointList endpoints={items} />
    </NextIntlClientProvider>,
  );
}

describe("EndpointList", () => {
  it("mostra a URL e o status traduzido", () => {
    renderList();
    expect(screen.getByText("https://api.acme.com/webhooks")).toBeInTheDocument();
    expect(screen.getByText(messages.endpoints.status.active)).toBeInTheDocument();
  });

  it("não mostra secret nenhum", () => {
    // A listagem é a resposta que mais aparece em tela, em cache de front e em
    // log de proxy. Revelar é da tela de detalhe.
    const { container } = renderList();
    expect(container.textContent).not.toContain("whsec_");
  });

  it("mostra o estado vazio quando não há endpoint", () => {
    renderList([]);
    expect(screen.getByText(messages.endpoints.empty)).toBeInTheDocument();
  });
});
```

Rode e veja falhar com `Failed to resolve import "./endpoint-list"`.

- [ ] **Step 11: Implementar a lista e a página, e ver passar**

`_components/endpoint-list.tsx`:

```tsx
import { useFormatter, useTranslations } from "next-intl";

import { Badge } from "@/components/ui/badge";
import type { Endpoint } from "@/features/endpoints/api/client";

export function EndpointList({ endpoints }: { endpoints: Endpoint[] }) {
  const t = useTranslations("endpoints");
  const format = useFormatter();

  if (endpoints.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("empty")}</p>;
  }

  return (
    <ul className="flex flex-col gap-3">
      {endpoints.map((endpoint) => (
        <li key={endpoint.id} className="flex flex-col gap-1 rounded border p-3">
          <span className="font-mono text-sm break-all">{endpoint.url}</span>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Badge>{t(`status.${endpoint.status}`)}</Badge>
            <span>
              {t("createdAt")} {format.dateTime(new Date(endpoint.created_at))}
            </span>
          </div>
        </li>
      ))}
    </ul>
  );
}
```

`page.tsx` **só compõe** — nenhum componente definido nele, que é a regra de `apps/CLAUDE.md`. Como ele precisa de estado (o secret recém-criado, e a contagem para o limite), a composição com estado fica num componente de cliente:

```tsx
// app/(dashboard)/endpoints/page.tsx
import { EndpointsScreen } from "./_components/endpoints-screen";

export default function EndpointsPage() {
  return <EndpointsScreen />;
}
```

```tsx
// app/(dashboard)/endpoints/_components/endpoints-screen.tsx
"use client";

import { useTranslations } from "next-intl";
import { useState } from "react";

import type { EndpointWithSecret } from "@/features/endpoints/api/client";
import { useEndpoints } from "@/features/endpoints/hooks/use-endpoints";

import { EndpointForm } from "./endpoint-form";
import { EndpointList } from "./endpoint-list";
import { EndpointSecret } from "./endpoint-secret";

// §4.28: 2 endpoints por application no plano free.
const PLAN_LIMIT = 2;

export function EndpointsScreen() {
  const t = useTranslations("endpoints");
  const { data: endpoints = [] } = useEndpoints();
  const [created, setCreated] = useState<EndpointWithSecret | null>(null);

  return (
    <main className="mx-auto flex max-w-2xl flex-col gap-6 p-4">
      <h1 className="text-2xl font-semibold">{t("title")}</h1>

      <EndpointForm
        isAtPlanLimit={endpoints.length >= PLAN_LIMIT}
        onEndpointCreated={setCreated}
      />

      {created ? (
        <section className="rounded border p-3">
          <h2 className="mb-2 text-sm font-medium">{t("secretLabel")}</h2>
          <EndpointSecret secret={created.secret} />
        </section>
      ) : null}

      <EndpointList endpoints={endpoints} />
    </main>
  );
}
```

O layout é **mobile first**: o estilo base é o de tela pequena, e `max-w-2xl` só limita a largura acima disso.

- [ ] **Step 12: Verificar tudo**

```bash
pnpm -r typecheck && pnpm -r lint && pnpm -r test && pnpm -r build
```

Esperado: os quatro sem erro, com os testes de `EndpointForm`, `EndpointSecret` e `EndpointList` verdes.

- [ ] **Step 13: Commit**

```bash
git add apps/dashboard/src
```

```
feat: criar a tela de endpoints
```

---

## Task 7: O teste de arquitetura e a documentação

**Files:**
- Create: `apps/dashboard/src/mocks/isolation.test.ts`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: tudo.

- [ ] **Step 1: Escrever o teste que mantém a promessa da remoção**

`apps/dashboard/src/mocks/isolation.test.ts`:

```ts
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { describe, expect, it } from "vitest";

const SRC = path.resolve(__dirname, "..");

async function sourceFiles(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const found = await Promise.all(
    entries.map(async (entry) => {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) return sourceFiles(full);
      return /\.tsx?$/.test(entry.name) ? [full] : [];
    }),
  );
  return found.flat();
}

describe("isolamento dos mocks", () => {
  it("só os route handlers do BFF importam de mocks/", async () => {
    // É o que mantém a promessa do README: apagar a pasta e editar dois
    // arquivos. Sem este teste, um import solto transforma a remoção numa
    // caça ao erro de compilação.
    const offenders: string[] = [];

    for (const file of await sourceFiles(SRC)) {
      const relative = path.relative(SRC, file).replaceAll("\\", "/");
      if (relative.startsWith("mocks/")) continue;
      if (relative.startsWith("app/api/")) continue;

      const content = await readFile(file, "utf8");
      if (/from ["']@\/mocks|from ["'][./]+mocks\//.test(content)) {
        offenders.push(relative);
      }
    }

    expect(offenders, "estes arquivos importam de mocks/ e não deveriam").toEqual([]);
  });
});
```

- [ ] **Step 2: Provar que o teste pega a violação**

Acrescente temporariamente `import { mockStore } from "@/mocks/store";` em `src/features/endpoints/api/client.ts` e rode:

```bash
pnpm --filter dashboard test -- isolation
```

Esperado: FAIL listando `features/endpoints/api/client.ts`. **Remova o import e rode até verde.**

- [ ] **Step 3: Atualizar a tabela de comandos**

Em `CLAUDE.md`, na tabela `## Comandos`, acrescente:

```markdown
| `pnpm install` | instala o workspace; exige pnpm via `corepack enable pnpm` |
| `pnpm --filter dashboard dev` | sobe o dashboard em `localhost:3000`, com BFF mockado |
| `pnpm -r test` | testes do front |
```

E, logo abaixo da tabela, a frase que quem for fazer deploy precisa ler:

```markdown
O dashboard desta release **não fala com a API**: o BFF devolve fixture, e os
dados são inventados. Conectar é spec própria — e é ela que traz o gate de
acesso, porque a tela aberta com dado real expõe secret de assinatura.
```

- [ ] **Step 4: Verificação manual completa**

```bash
pnpm --filter dashboard dev
```

Na tela, em `localhost:3000/endpoints`:

1. A lista mostra o endpoint do fixture, **sem secret**.
2. Cadastrar `https://api.exemplo.com/hooks` → aparece na lista, secret mascarado.
3. Revelar o secret → valor completo; ocultar → volta a mascarar.
4. Cadastrar `http://acme.com/x` → campo marcado **sem** ida ao servidor.
5. Cadastrar `https://10.0.0.1/x` → campo marcado com a mensagem de `EPT-VAL-002`.
6. Cadastrar o terceiro → botão desabilitado, mensagem de `RTL-LMT-001`.
7. Corrigir a URL de um endpoint → lista reflete, **secret não muda**.

Capture as saídas e diga o que não conseguiu exercitar.

- [ ] **Step 5: Suíte completa**

```bash
pnpm install --frozen-lockfile
pnpm -r typecheck
pnpm -r lint
pnpm -r test
pnpm -r build
go test -p 1 ./...
npx --yes openapi-typescript@<versão> contracts/openapi.yaml -o apps/dashboard/src/lib/api-types.ts
git diff --exit-code
```

Esperado: tudo verde, sem diff.

- [ ] **Step 6: Commit**

```bash
git add apps/dashboard CLAUDE.md
```

```
feat: isolar os mocks e documentar a remoção
```

Este é o `feat:` que corta a `v0.4.0`.

---

## Encerramento da spec

- [ ] **Escrever o `result.md`** com o template de `docs/specs/_template_/result.md`.

Candidatos prováveis de divergência, porque o plano os assume sem poder verificar:

- a forma exata dos tipos que `openapi-typescript` gera — `components["schemas"]["Endpoint"]` é previsão;
- a API do `next-intl` na versão instalada, em especial o plugin do `next.config.ts`;
- a assinatura de `params` nos route handlers, que virou `Promise` no Next 15;
- se `vitest run` sem teste algum falha em `apps/dashboard` no Step 8 da Task 1;
- se o `shadcn init` aceita o layout com `src-dir` sem ajuste manual.

- [ ] **Atualizar o índice** em `docs/specs/README.md`: status da 004 para `implementada`.
