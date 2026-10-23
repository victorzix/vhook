# 004 — Esqueleto do dashboard · Resultado

| | |
|---|---|
| **Release** | `v0.4.0` |
| **Spec** | [`spec.md`](spec.md) |
| **Plano** | [`plan.md`](plan.md) |

---

## Divergências da spec

Nenhuma no comportamento observável: as sete linhas da tabela "Tela de Endpoints"
e as quatro rotas do BFF saíram como escritas.

**Mas três delas o plano não tinha.** A spec promete e o plano omitiu, sem dizer
que omitia — e como as chaves de i18n correspondentes (`copy`, `copied`, `edit`,
`save`, `cancel`) estavam no plano e nos quatro locales, o buraco não aparecia em
nenhum teste. Foram implementadas durante a Task 7, ao conferir a spec contra o
que existia:

| A spec pedia | O plano entregava | O que foi feito |
|---|---|---|
| "Cadastrar o terceiro → botão desabilitado **com a mensagem de `RTL-LMT-001`**" | Só o botão desabilitado | Parágrafo com o texto do catálogo acima do botão quando `isAtPlanLimit` |
| "Revelar o secret → valor completo aparece; **há botão de copiar**" | Só revelar e ocultar | Botão de copiar em `EndpointSecret`, copiando o valor **completo** e não o mascarado |
| "Corrigir a URL → a lista reflete a nova URL; o secret não muda" | `useUpdateEndpointUrl` e o `PATCH` do BFF existiam, **sem nenhuma UI que os chamasse** | `EndpointUrlEditor`, um componente por linha da lista |

A lição é sobre o formato do plano, não sobre estas três telas: **plano que
transcreve código pronto não tem como acusar o que esqueceu.** Os testes do
plano testavam o que o plano escreveu, então passavam. O que pegou foi reler a
spec no fim — e isso é sorte, não processo.

## Divergências do plano

| Plano dizia | Ficou | Por quê |
|---|---|---|
| `corepack enable pnpm` | `npm i -g pnpm`, versão **12.15.0** | `corepack` não existe neste Node |
| Node 22 no CI | **24.22.1**, fixado em `.nvmrc`, `engines` e `node-version-file` | O jsdom do Vitest carrega `undici@8`, que usa `zlib.createZstdDecompress` — só existe do Node 22.15 em diante. O worker morre com `Cannot read properties of undefined (reading 'bind')`, que não parece erro de versão |
| `onlyBuiltDependencies` | `allowBuilds` no `pnpm-workspace.yaml` | Nome do campo no pnpm 12. Sem ele o install falha com `ERR_PNPM_IGNORED_BUILDS`, aqui e no CI |
| `create-next-app --no-turbopack` | Turbopack mantido | A flag não existe mais no Next 16, e o Tailwind 4 do scaffold passa pelo loader `@tailwindcss/turbopack` |
| `"lint": "next lint"` | `"lint": "eslint"` | `next lint` foi removido no Next 16 |
| `"typecheck": "tsc --noEmit"` | `"next typegen && tsc --noEmit"` | `LayoutProps` é tipo gerado em `.next/types`, e o CI roda `typecheck` **antes** do `build`. Sem o typegen, checkout limpo falha |
| `openapi-typescript@7.9.1` | `7.13.0` | A versão do plano é antiga |
| `vitest ^3` no `i18n` | `^5`, igual ao app | Duas majors no mesmo repo não se justificam |
| `vitest.config.ts` | `.mts` nos dois pacotes | Vite 8 avisa sobre config ESM carregada como CJS |
| `z.string().url()` | `z.url()` | Deprecado no zod 4 — está escrito no `.d.ts` instalado |
| `shadcn add … form card table` | Só `button`, `input`, `badge` | `form` não existe no registry `base-nova`; `card` e `table` ninguém usava. Mesmo argumento que o plano usou para barrar o `framer-motion` |
| Task 1 podia terminar com `pnpm -r test` vermelho, registrando pendência | Terminou verde | O `CLAUDE.md` proíbe encerrar com teste vermelho. Em vez do atalho, escrevi o primeiro teste do app ali: `src/lib/shared-catalog.test.ts`, que prova a costura do workspace |

**O que o plano mandou verificar, e deu certo como ele previa:** `params`
continua `Promise` nos route handlers do Next 16; exportar `errorEnvelope` de
dentro de um `route.ts` passa pela validação de exports; o `shadcn init`
aceitou o layout com `src-dir` sem ajuste; os tipos gerados saíram como
`components["schemas"]["Endpoint"]`, com opcional em `?` e não `| undefined`.

## Decisões que só apareceram implementando

**`create-next-app` cria um workspace aninhado.** Ele escreveu
`pnpm-workspace.yaml`, `pnpm-lock.yaml` e `node_modules` próprios dentro de
`apps/dashboard`, além de duplicar `packageManager`. Removidos — sem isso o app
não é membro do monorepo, e o typecheck acusa dois `next` de identidades
diferentes.

**`shadcn` 4 mudou de base.** O preset padrão (`base-nova`) usa **Base UI**, não
Radix, e traz o pacote `cn` — o `src/lib/utils.ts` é só um re-export. Nada disso
muda a regra de `apps/CLAUDE.md`: os componentes continuam **copiados** para
`src/components/ui/`, e promovê-los para `packages/ui` segue sendo `git mv`. O
CLI também pôs a si mesmo em `dependencies`; movido para `devDependencies`.

**A mensagem de erro do campo tem dois donos, e isso é um bug esperando.** O
plano renderizava `t(errors.url.message)`. Funciona para o zod, que escreve uma
**chave**; quebra para a API, que escreve **texto que o catálogo já resolveu** —
e o resultado é `endpoints.<a frase inteira>` em tela com `MISSING_MESSAGE` no
console. Renderiza sem quebrar, que é exatamente por que passaria despercebido.
Resolvido com `t.has()` antes de traduzir.

**`isPlanLimitError` continua sem uso.** O plano dizia que a página o usaria; a
página usa a constante `PLAN_LIMIT`. A função está testada e exportada, e quem
vai precisar dela é a spec de conexão — quando o limite deixar de ser adivinhado
no cliente e passar a vir da resposta. Registrado em "Pendente" em vez de
removido.

**`outputFileTracingRoot` no `next.config.ts`.** Num monorepo, sem apontar o
Next para a raiz, o `output: "standalone"` copia a árvore errada de
`node_modules`.

## Evidência de que funciona

**Testes — 38 em dois pacotes:**

```
i18n            3 testes   (paridade dos quatro locales)
apps/dashboard  35 testes  (10 arquivos)
```

Os que provam invariante, e não só ausência de erro:

- **A paridade do catálogo foi vista vermelha.** Removendo `EPT-VAL-003` de
  `errors.fr.json`: `locale fr divergiu de pt-BR`, apontando a chave.
- **A paridade das mensagens de interface foi vista vermelha.** Removendo
  `copied` de `messages/fr.json`: `locale fr divergiu`, apontando
  `endpoints.copied`.
- **O teste de arquitetura foi visto vermelho.** Com `import { mockStore } from
  "@/mocks/store"` em `features/endpoints/api/client.ts`, ele listou
  exatamente esse arquivo. É o que mantém a promessa de "apagar a pasta".
- **A costura do workspace foi vista vermelha.** Removendo `@vhook/i18n` das
  dependências do app: `Failed to resolve import "@vhook/i18n"`. Sem este teste,
  `transpilePackages` quebrado só apareceria em tela.
- **O bug da dupla tradução foi reproduzido em teste antes de ser corrigido** —
  `Unable to find an element with the text: A URL aponta para um endereço de
  rede não permitido.`, encontrando `endpoints.A URL aponta…`.
- **O formulário não chama o BFF para `http://`**, verificado por espião no
  `fetch` no teste e pelo painel de rede do navegador: só o `GET` da listagem.

**Manual — os sete cenários da spec, no navegador:**

```
abrir a tela           → fixture listado, sem secret, "Ativo", data em pt-BR
http://acme.com/x      → campo marcado, nenhum POST na rede
https://10.0.0.1/x     → "A URL aponta para um endereço de rede não permitido."
https://api.exemplo…   → 201, lista atualiza, secret whsec_••••••••4pac
revelar / ocultar      → valor completo e volta a mascarar
copiar                 → botão vira "Copiado"
terceiro cadastro      → botão desabilitado + "Limite de endpoints do plano atingido."
corrigir URL pela tela → lista reflete; GET /{id} mostra o MESMO secret de antes
```

E o BFF direto, por HTTP, cobrindo o que a tela não alcança:

```
POST duplicando o seed  → 409 EPT-CFL-001
GET  id inexistente     → 404 EPT-NFD-001
PATCH                   → 200, resposta SEM secret
GET  lista              → 200, nenhum item com secret
```

Nenhuma resposta de erro carregou `message`; todas trouxeram `correlation_id`.

**Determinismo** — `openapi-typescript` regerado duas vezes, `git diff
--exit-code` limpo. É o que dá valor à barreira do CI.

**O que não foi exercitado:** o `RTL-LMT-001` vindo do servidor. O cliente
desabilita o botão antes, então o código só chega à tela se o limite do cliente
e o do servidor divergirem. O caminho do servidor está provado por HTTP (403) e
o tratamento genérico cobriria a exibição.

## Contratos alterados

Nenhum. `contracts/openapi.yaml` não mudou — esta spec **consome** o contrato da
003, e é esse o ponto de gerar `apps/dashboard/src/lib/api-types.ts` a partir
dele.

`Makefile` ganhou a terceira linha no alvo `generate`, por `npx` com versão
fixada: o job Go do CI roda `make generate` e não faz `pnpm install`.

## Pendente

| Item | Para onde foi |
|---|---|
| Conectar o BFF à API Go | Spec própria. O roteiro está em `apps/dashboard/src/mocks/README.md`, e o teste de arquitetura confirma a remoção |
| Gate de acesso no proxy do Coolify | Mesma spec. **Hoje a tela é aberta de propósito** — com dado inventado é inofensiva; com dado real expõe secret de assinatura |
| `isPlanLimitError` sem uso | Spec de conexão, quando o limite vier da resposta em vez de uma constante no cliente |
| "Formatação do identificador externo para exibição" | Previsto na seção de provas da spec, nunca detalhado no plano nem necessário pela tela. Volta quando houver tela de detalhe |
| `packages/ui` | Quando `apps/commercial` existir e houver segundo uso |
| Playwright | Junto do feed por SSE, que é o fluxo que `apps/CLAUDE.md` nomeia para ele |
| `zustand` | Quando aparecer estado de UI que o justifique |
| Trocar o locale em tempo de execução | A spec pede "trocar o locale e ver as mensagens mudarem" na verificação manual; não há seletor, e `request.ts` fixa `pt-BR`. A troca é da spec de login, que traz o locale da sessão — a paridade dos quatro locales está provada por teste |
