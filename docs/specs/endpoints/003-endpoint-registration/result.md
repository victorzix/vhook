# 003 — Cadastro de endpoints · Resultado

| | |
|---|---|
| **Release** | `v0.3.0` |
| **Spec** | [`spec.md`](spec.md) |
| **Plano** | [`plan.md`](plan.md) |

---

## Divergências da spec

Nenhuma. Todo comportamento observável saiu como especificado: as quatro rotas aninhadas, os nove códigos de erro com os status descritos, a listagem sem secret, o 404 para recurso de outro tenant, o 403 no estouro do limite e o 409 na URL duplicada.

As divergências foram **do plano**, e três delas eram erros meus com consequência real. Estão abaixo.

## Divergências do plano

| Plano dizia | Ficou | Por quê |
|---|---|---|
| Remover `addr.Unmap()` faz `::ffff:10.0.0.1` falhar | **Não faz.** Os casos que dependem da linha são `::ffff:100.64.0.1` e `::ffff:0.0.0.0` | Os predicados do `net/netip` — `IsPrivate`, `IsLoopback`, `IsLinkLocalUnicast` — já desmapeiam 4-em-6 por dentro. Quem depende do nosso `Unmap` é `IsUnspecified`, que não desmapeia, e os prefixos de CGNAT e `0.0.0.0/8`, que estão atrás de uma guarda `Is4()` falsa para 4-em-6 |
| `TestHealthSatisfiesTheGeneratedInterface` continua em `internal/obs` | **Movido** para `cmd/api` como `TestTheAPIServerSatisfiesTheGeneratedInterface`, afirmando `var _ openapi.ServerInterface = apiServer{}` | A `ServerInterface` cresceu de 3 para 7 métodos, e `*obs.Health` sozinho nunca mais a satisfaz. Nenhuma task do plano mencionava esse arquivo |
| `openapi.GetSwagger()` | `openapi.GetSpec()` | `GetSwagger` existe mas está deprecada — sobrevive por compatibilidade com o nome antigo do `openapi3.T` |
| Rota fora do contrato vira 422 `SYS-VAL-001` | **404**, como o `chi` já respondia antes desta spec | O `ErrorHandlerWithOpts` entrega `StatusCode: 404` e `MatchedRoute == nil` para caminho inexistente. Mapear isso para `MalformedID` inventaria um código de erro para "essa rota não existe". Efeito colateral aceito: método não permitido em rota existente passa a 404 em vez de 405 |
| O teste ponta a ponta não depende de rede | Depende, e foi corrigido: `api.exemplo.com` entrou na `ssrfAllowlist` do teste | Literais de IP não precisam de DNS, mas um hostname precisa. A allowlist dispensa a resolução **sem afrouxar o que o teste prova**: o `http://` continua recusado, porque a allowlist não perdoa esquema |
| `nethttp-middleware` permanece no `go.mod` depois da Task 5 | Removido pelo `go mod tidy` e readicionado na Task 8 | Nada o importava até o wiring existir. `go mod tidy` faz o que promete |
| `ApplicationId` e `EndpointId` seriam tipos nomeados | Alias de `string` | Consequência que vale registrar: **não há checagem de tipo**. Passar um id de application onde se espera o de endpoint compila. Quem pega é o parse por prefixo do `internal/ids`, em runtime |

Os nomes que o `sqlc` gerou acertaram todos os seis, e os tipos vieram como a spec 002 já ensinara: `pgtype.UUID`, `Url` em vez de `URL`, `CreatedAt` como `pgtype.Timestamptz`.

## Decisões que só apareceram implementando

**`DoNotValidateServers: true` muta o spec.** O middleware zera `spec.Servers` no documento recebido. É seguro porque `openapi.GetSpec()` decodifica um `*openapi3.T` novo a cada chamada, sem cache compartilhado — mas seria um bug silencioso se algum dia o spec passasse a ser memoizado.

**O `ErrorHandlerFunc` do wrapper gerado precisa ser fornecido.** Sem ele, um parâmetro de caminho malformado responde `http.Error(w, err.Error(), 400)` — texto livre, status escolhido pela biblioteca, fora do envelope de erro do projeto. É o tipo de default que só aparece quando alguém manda um id quebrado.

**Uma única função de wiring, chamada por produção e por teste.** O plano previa `buildRouterForTest`; virou `buildRouter`, usada por `main.go`, `server_test.go` e `endpoints_test.go`. Router de teste que difere do de produção transforma o teste ponta a ponta numa afirmação sobre código que ninguém roda.

## Evidência de que funciona

**Testes** — 243 em 16 pacotes:

```
ok  github.com/victorzix/vhook/cmd/api             68.378s
ok  github.com/victorzix/vhook/internal/endpoints  47.893s
ok  github.com/victorzix/vhook/internal/store      56.928s
ok  github.com/victorzix/vhook/internal/dispatch    0.090s
ok  github.com/victorzix/vhook/internal/secrets     0.100s
ok  github.com/victorzix/vhook/internal/httpauth    0.071s
```

Os que provam invariante, e não só ausência de erro:

- **A trava foi vista vermelha.** Movendo `lockApplication` para depois da contagem, **5 das 6 criações simultâneas passaram do limite de 2** — e os outros 12 testes de `endpoints` continuaram verdes. É exatamente o ponto: sem esse teste, a inversão não daria sintoma nenhum.
- **A autenticação foi vista vermelha.** Trocando o validador do contrato por um middleware no-op, as quatro rotas de management serviram **200 a qualquer um**, sem `Authorization`. É a prova de que o middleware é o que protege, e não alguma coincidência de roteamento.
- **O AAD foi visto vermelho.** Passando `nil` no lugar do `endpoint_id`, `Open` com o id de **outro** endpoint decifrou normalmente e devolveu `err = nil`.
- **O `Unmap` foi visto vermelho** — depois de corrigida a tabela de casos, ver acima.
- **O teste de completude do i18n acusou 36 vezes** — nove códigos × quatro locales — antes das traduções entrarem. Terceira vez que a barreira da spec 001 paga.
- **O secret decifrado do banco é igual ao que o `POST` devolveu**, usando o `endpoint_id` como AAD. Sem isso, o handler poderia devolver uma chave e gravar outra, e só a spec de disparo descobriria — como assinatura que o cliente rejeita.

**Manual** — quatorze cenários exercitados contra a `api` de verdade:

```
POST válido                        → 201 com secret whsec_…
GET sem Authorization              → 401 AUT-CRD-001
POST http://169.254.169.254/…      → 422 EPT-VAL-001
POST https://10.0.0.1/hooks        → 422 EPT-VAL-002
POST host que não resolve          → 422 EPT-VAL-003
GET /v1/applications/app_naoehumid → 422 SYS-VAL-001
GET lista                          → 200, sem "secret"
POST https://sink/hooks            → 201  (allowlist libera faixa privada)
POST terceiro endpoint             → 403 RTL-LMT-001
GET detalhe                        → 200, mesmo secret da criação
PATCH url                          → 200, sem secret, url trocada
GET /v1/nao-existe                 → 404
boot sem VHOOK_ADMIN_TOKEN         → CFG-VAL-001, exit 1, antes de abrir porta
boot sem VHOOK_MASTER_KEY          → CFG-VAL-001, exit 1
```

Nenhuma resposta de erro carregou `message`; todas trouxeram `correlation_id`. O log registrou método, caminho, status e duração — nenhum secret, nenhum token.

E a verificação que fecha a cifra, no `psql`:

```sql
SELECT count(*) FROM endpoints
 WHERE encode(secret_encrypted, 'escape') LIKE '%whsec%';
-- 0
```

**Um cenário não exercitado manualmente:** o 409 `EPT-CFL-001`. O tenant já estava com dois endpoints quando a URL duplicada foi tentada, então o limite do plano respondeu antes. O caminho está coberto por `TestCreateRefusesADuplicateURL`, com application nova.

**Determinismo do código gerado** — regerar `sqlc` e `oapi-codegen` não muda um byte, verificado por checksum.

## Contratos alterados

`contracts/openapi.yaml`, editado durante a aprovação da spec:

- Quatro operações novas sob `/v1/applications/{application_id}/endpoints`, todas com `security: [AdminToken]`.
- Schemas novos: `ApplicationId`, `EndpointId`, `EndpointStatus`, `Endpoint`, `EndpointWithSecret`, `EndpointList`, `CreateEndpointRequest`, `UpdateEndpointRequest`.
- Respostas novas: `Forbidden`, `NotFound`, `Conflict`.
- `EndpointWithSecret` **repete** os campos de `Endpoint` em vez de compor com `allOf`. Menos DRY de propósito: composição gera nome de tipo imprevisível, e este contrato prefere geração determinística — mesmo motivo que promoveu `ReadyChecks` a schema nomeado na 001.

`contracts/oapi-codegen.yaml` passou a gerar o spec embutido (`embedded-spec: true`), sem o qual o middleware não teria como saber quais operações exigem `AdminToken`.

## Pendente

| Item | Para onde foi |
|---|---|
| `allow_insecure` para permitir `http://` no destino | Roadmap da spec. Booleano explícito, nunca default, **persistido** — numa investigação de vazamento, "era `http` por opt-in" precisa estar na linha do banco, não num submit que sumiu |
| Cadastrar endpoint cujo DNS ainda não resolve | Roadmap, com estado `unverified` e revalidação |
| `DELETE /endpoints/:id` | Junto com a decisão sobre o histórico de entregas do endpoint apagado |
| Taxa de sucesso 24h na listagem | Quando `deliveries` tiver linha |
| `POST /:id/enable` e desativação manual | Spec do circuit breaker, dona de `status`, `consecutive_failures` e `disabled_at` |
| `GET /v1/applications` | Spec de login: o dashboard precisa para desenhar o seletor de projeto |
| Rate limit | Spec de ingress, onde defende algo concreto |
| **Flakiness do testcontainers com pacotes em paralelo** | **Dívida de infraestrutura de teste, registrada abaixo** |

### A flakiness, com a medição que existe

Durante a execução da Task 8, `go test -shuffle=on ./...` com paralelismo padrão falhou **duas vezes em três** com erro do reaper do testcontainers (`unexpected container status "created"`), enquanto `-p 1` passou sempre.

Medido de novo depois, com o Docker limpo: **duas rodadas completas, zero falha, zero erro de reaper.**

A leitura mais provável é **dependência de carga**, não defeito determinístico. As falhas apareceram ao fim de uma sessão longa, com dezenas de containers já criados e destruídos; num Docker recém-acordado o mesmo comando passa.

Não é regressão desta spec: são três pacotes de integração — `store`, `endpoints` e `cmd/api` — disputando o Docker, e o padrão "um container por teste" veio da spec 001.

**Nada foi mudado no CI**, que roda `go test -race -shuffle=on ./...` sem `-p 1`. O motivo de não agir preventivamente: cada job do GitHub Actions começa com Docker limpo, que é a condição em que a medição passou; e serializar com `-p 1` custaria wall-clock real, com três pacotes de integração em fila.

**O que fazer quando o CI acusar:** a medição acima é o ponto de partida. As opções são serializar (`-p 1`, simples e lento) ou compartilhar container entre testes do mesmo pacote (rápido e acopla os testes entre si). O tradeoff não vale ser decidido antes de existir uma falha real para explicar.
