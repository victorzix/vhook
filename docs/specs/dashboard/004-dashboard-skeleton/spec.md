# 004 — Esqueleto do dashboard

| | |
|---|---|
| **Status** | aprovada |
| **Release alvo** | `v0.4.0` |
| **Plano** | [`plan.md`](plan.md) |
| **Resultado** | [`result.md`](result.md) |

---

## Problema

Não existe front, e não é só ausência de tela. Falta o workspace pnpm, falta o BFF que [§4.22](../../../ARCHITECTURE.md) exige, falta a geração de tipos TS que a [spec 001](../../platform/001-walking-skeleton/spec.md) adiou dizendo "entra com o primeiro app", e falta o lado TypeScript do catálogo de erros de [§4.29](../../../ARCHITECTURE.md).

O job de front do CI **nunca rodou**: ele sai limpo por ausência de `pnpm-workspace.yaml`, e vai continuar saindo até existir um app.

Das quatro telas de [§7](../../../ARCHITECTURE.md), três dependem de `deliveries`, que não tem linha e não terá até a spec de entrega. A de Endpoints é a única cuja API existe inteira, desde a [spec 003](../../endpoints/003-endpoint-registration/spec.md).

## Escopo

**Entra**

| Peça | Conteúdo |
|---|---|
| `pnpm-workspace.yaml` | um membro, `apps/dashboard`. É o que tira o job de front do silêncio |
| `apps/dashboard` | Next App Router, TypeScript, Tailwind, shadcn instalado **dentro do app** |
| `i18n/package.json` | torna consumível pelo TS o catálogo que já é compartilhado |
| BFF | route handlers do Next, **devolvendo fixture** |
| `apps/dashboard/src/mocks/` | fixtures tipados pelo contrato e estado em memória |
| `make generate` | `openapi-typescript` ao lado de sqlc e oapi-codegen |
| Tela de Endpoints | listar, cadastrar, revelar secret, corrigir URL |
| Teste de paridade de chave i18n | nos quatro locales, como [`apps/CLAUDE.md`](../../../../apps/CLAUDE.md) exige |

**Não entra**

- **Conexão com a API Go.** Decisão desta spec: o BFF devolve fixture. Ver "O mock mora no BFF" abaixo.
- **`packages/ui`** — existe **um** app. A escada de promoção do próprio `apps/CLAUDE.md` manda promover no segundo uso: *"Não criar em `shared/` 'porque vai ser reutilizado'. Promove quando o segundo uso aparecer — antes disso é adivinhação, e componente compartilhado prematuro nasce com abstração errada."*
- **`zustand`** — a tela de Endpoints não tem estado de UI que justifique store. Instalar biblioteca sem uso é decidir a forma antes de conhecer o problema.
- **Playwright** — `apps/CLAUDE.md` nomeia o fluxo que ele deve cobrir: *"uma entrega mudando de estado sozinha via SSE"*. Esse fluxo não existe. Instalar agora significaria baixar browser no CI para testar um formulário que Testing Library cobre. Entra na spec do feed.
- **`VHOOK_ADMIN_TOKEN` e `VHOOK_APPLICATION_ID` no dashboard** — sem conexão, não há o que autenticar nem para onde apontar. Ambiente só para o que é usado ([§4.25](../../../ARCHITECTURE.md)).
- **`apps/commercial`** — propósito diferente, regras próprias.
- **Feed de entregas, detalhe e playground** — dependem de `deliveries`.

## O mock mora no BFF, não antes dele

```
browser → /api/endpoints → [ fixture ]        ← esta release
browser → /api/endpoints → fetch(API Go)      ← spec da conexão
```

O route handler existe e continua sendo a única coisa que o browser chama. O que muda depois é **só o corpo dele**.

**Por que aqui e não na camada `features/endpoints/api/`:** assim a costura fica exatamente onde ela vai ficar de verdade. Hooks, componentes e tratamento de erro nascem escritos contra o formato final e **nunca precisam mudar**. Trocar mock por API é editar três route handlers e apagar uma pasta.

Se o mock morasse em `features/`, o BFF não existiria ainda, e a tela estaria construída contra algo provisório — que é o tipo de código que "depois a gente ajusta" e nunca ajusta.

### Onde os mocks moram

```
apps/dashboard/src/mocks/          ← a pasta inteira é descartável
├── README.md                      o que apagar e o que editar para conectar
├── fixtures/endpoints.ts          dados, tipados com os tipos gerados
└── store.ts                       estado em memória
```

**Só os route handlers importam de `mocks/`.** Nada em `features/`, nada em `app/(dashboard)/`. Há teste de arquitetura verificando isso — sem ele, um import solto transforma "apagar a pasta" em caça ao erro de compilação.

O `README.md` da pasta lista os arquivos a editar para conectar. É a instrução de remoção escrita enquanto o contexto está fresco, não seis meses depois.

### Os fixtures são tipados pelo contrato

Os fixtures usam os tipos que `openapi-typescript` gera de `contracts/openapi.yaml`. Se o contrato mudar, **o build quebra nos fixtures**.

É a diferença entre "mock que reflete a API" e "mock que refletia a API há três semanas". Sem essa amarração, um mock à mão vira ficção com aparência de verdade, e a tela é construída contra um formato que a API não tem.

### Estado em memória

`store.ts` guarda os endpoints numa variável de módulo. Cadastrar acrescenta, editar altera, listar reflete. Reinicia com o servidor e é por instância — irrelevante agora, e some junto com o mock.

Sem isso a tela seria uma lista estática onde o botão de cadastrar não produz efeito visível, e não daria para demonstrar o fluxo.

**Os fixtures incluem os caminhos de erro.** Uma URL que devolve `EPT-VAL-002`, e o terceiro cadastro devolvendo `RTL-LMT-001`. É o que torna o tratamento de erro exercitável à mão — senão ele é escrito e nunca visto funcionando.

## Comportamento observável

### Tela de Endpoints

| Ação | Resultado |
|---|---|
| Abrir a tela | Lista os endpoints do fixture, com URL, status e data |
| Cadastrar URL `https://` válida | Endpoint aparece na lista; o secret é mostrado **mascarado**, com botão de revelar |
| Cadastrar URL sem `https` | O campo é marcado antes do submit, por validação zod local |
| Cadastrar URL que o mock recusa por faixa | Campo marcado com a mensagem de `EPT-VAL-002`, vinda do catálogo |
| Cadastrar o terceiro | Botão desabilitado com a mensagem de `RTL-LMT-001` |
| Revelar o secret | Valor completo aparece; há botão de copiar |
| Corrigir a URL | A lista reflete a nova URL; **o secret não muda** |

O secret aparece **mascarado por padrão**. §4.28 permite revelá-lo quantas vezes se quiser, e a rota de detalhe traz — mas este dashboard vai ser mostrado para gente. Secret em tela por padrão vaza em screenshot, em compartilhamento de tela e em gravação. Mascarar não protege contra quem tem acesso; protege contra quem está olhando junto.

### Rotas do BFF

```
GET   /api/endpoints
POST  /api/endpoints
GET   /api/endpoints/{id}
PATCH /api/endpoints/{id}
```

**O `application_id` não aparece aqui.** [§4.34](../../../ARCHITECTURE.md) o exige no caminho da **API Go**, e é o BFF quem o acrescenta — hoje um valor de fixture, amanhã o da sessão do usuário. É justamente o que o BFF existe para fazer: o front não sabe de tenant.

## Estrutura

Seguindo `apps/CLAUDE.md`:

```
apps/dashboard/src/
├── app/
│   ├── api/endpoints/route.ts          BFF: lista e cria
│   ├── api/endpoints/[id]/route.ts     BFF: detalhe e patch
│   └── (dashboard)/endpoints/
│       ├── page.tsx                    SÓ composição
│       └── _components/                usados só nesta rota
├── features/endpoints/
│   ├── api/                            chamadas ao route handler
│   ├── hooks/                          TanStack Query
│   ├── lib/                            lógica pura — onde o red-green mora
│   └── schemas/                        zod
├── mocks/                              descartável
└── lib/
```

`page.tsx` compõe e nada mais. A lógica pura vive em `features/endpoints/lib/`, testável em milissegundos sem React — é o `internal/core` do front.

Nenhum componente nasce em `components/shared/`: há uma rota só, e a escada de promoção do `apps/CLAUDE.md` sobe no segundo uso.

## O catálogo de erros vira pacote de workspace

`i18n/` já é compartilhado entre Go e TS por decisão de §4.29 — `go:embed` de um lado, import do outro. Falta só o `package.json` que o torna resolvível pelo pnpm.

**Isso não contradiz a recusa do `packages/ui`.** Lá o argumento é não inventar abstração antes do segundo consumidor. Aqui o segundo consumidor **já existe**: é o Go, desde a spec 001. Só falta tornar consumível o que já é compartilhado.

**Descartado:** copiar os JSON para dentro do app num passo de build. Cria duas cópias do mesmo texto, e a que fica velha é justamente a que ninguém olha.

## Erros, ponta a ponta

A API devolve **só código** (§4.29). O BFF repassa o envelope sem inventar texto. O front traduz pelo mesmo `i18n/errors.<locale>.json` que o Go usa, e usa `details[]` para marcar o campo — não para montar frase.

| Código | O que a tela faz |
|---|---|
| `EPT-VAL-001` | Marca o campo URL |
| `EPT-VAL-002` | Marca o campo URL, com texto diferente |
| `EPT-VAL-003` | Marca o campo URL, com texto diferente |
| `RTL-LMT-001` | Desabilita o botão de cadastrar |
| `EPT-CFL-001` | Marca o campo URL como duplicada |

Os três `EPT-VAL-*` produzirem textos diferentes é o motivo de eles terem sido cunhados separados na spec 003: um código só forçaria a tela a adivinhar qual mensagem mostrar.

**Nenhuma string visível em JSX.** Todo texto passa pelo i18n, inclusive `aria-label`, `placeholder` e mensagem de validação. Chave nova entra nos quatro locales no mesmo commit, e há teste que falha se qualquer chave existir num locale e faltar noutro.

## Tipos

`openapi-typescript` gera de `contracts/openapi.yaml` dentro de `make generate`, ao lado de sqlc e oapi-codegen. O CI já compara `make generate` com `git diff --exit-code`, então tipo TS atrasado vira PR vermelho — a mesma barreira que o Go tem desde a 001.

Zod valida **entrada do usuário**, não redeclara o contrato. O submit é tipado com o tipo gerado, e o TypeScript acusa se o schema divergir do que a API aceita.

## O dashboard é deliberadamente aberto

O BFF protege o **token**: ele fica server-side e o browser nunca o vê. Ele **não protege a tela**. Quem alcançar a URL cadastra, revela e edita.

Nesta release isso é inofensivo — os dados são inventados e não há secret real.

**Vira condição para a spec da conexão:** no dia em que o BFF falar com a API de verdade, a tela aberta passa a expor secret de assinatura real a qualquer um que abra a URL. Nesse momento o gate deixa de ser opcional, e a saída barata é basic auth no proxy do Coolify — uma linha de configuração, sem código nosso, que some quando o login existir.

Registrado aqui porque quem for fazer aquele deploy precisa desta informação, e ela não pode ficar só na conversa que gerou esta spec.

## Como se prova que funciona

**Unidade — Vitest, sem React, em `features/endpoints/lib/`**

- Mascaramento do secret: mostra prefixo e últimos caracteres, esconde o meio, e nunca devolve string vazia para entrada válida.
- Mapeamento de código de erro para campo de formulário, incluindo código desconhecido caindo em erro genérico.
- Formatação do identificador externo para exibição.

**Componente — Testing Library, contra BFF mockado**

- A lista renderiza os endpoints e **não mostra o secret**.
- O formulário recusa URL sem `https` **antes** de submeter — validação local, sem ida ao BFF.
- `EPT-VAL-002` marca o campo com a mensagem do catálogo, não com texto embutido.
- `RTL-LMT-001` desabilita o botão de cadastrar.
- O secret aparece mascarado; o botão de revelar mostra o valor completo.
- Corrigir a URL não altera o secret exibido.

**Arquitetura**

- Nenhum arquivo fora de `app/api/` importa de `mocks/`. É o teste que mantém a promessa de "apagar a pasta e conectar".

**i18n**

- Todo código de erro registrado em `internal/errs` tem entrada nos quatro locales.
- Nenhuma chave existe num locale e falta noutro.

**Manual**

`pnpm dev`, abrir a tela, cadastrar duas URLs válidas, tentar `http://`, tentar a URL que o fixture recusa por faixa, tentar a terceira para ver o limite, revelar o secret, corrigir uma URL e confirmar que o secret não mudou. Trocar o locale e ver as mensagens mudarem.

## Decisões arquiteturais geradas

**[§4.35](../../../ARCHITECTURE.md) — Next.js para o front, com a superfície de CVE analisada.** Registra a comparação com Angular e Vue, por que o BFF de §4.22 é o critério estrutural, e as três regras que a análise de ameaça impõe.

## Empurrado para o roadmap

- **Conectar o BFF à API Go** — spec própria, pequena: apagar `mocks/`, editar três route handlers, acrescentar `VHOOK_ADMIN_TOKEN` e `VHOOK_APPLICATION_ID`. É a spec que traz junto o gate no proxy.
- **`packages/ui`** — quando o `apps/commercial` existir e houver segundo uso de verdade.
- **Playwright** — junto do feed por SSE, que é o fluxo que `apps/CLAUDE.md` nomeia para ele.
- **`zustand`** — quando aparecer estado de UI que o justifique.
- **Feed de entregas, detalhe e playground** — dependem de `deliveries`.
- **Login** — quando reabrir §4.14. É o que faz `VHOOK_APPLICATION_ID` sumir.
