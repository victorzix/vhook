# 002 — Bootstrap de tenancy · Resultado

| | |
|---|---|
| **Release** | `v0.2.0` |
| **Spec** | [`spec.md`](spec.md) |
| **Plano** | [`plan.md`](plan.md) |

---

## Divergências da spec

**Uma, e a spec estava errada.**

| Spec dizia | Ficou | Por quê |
|---|---|---|
| Duas execuções simultâneas: "a transação e o `UNIQUE` serializam: uma cria, a outra falha com violação de constraint" | **Advisory lock de escopo de transação**, tomado antes do `count` | A afirmação da spec era falsa. Em `READ COMMITTED` as execuções concorrentes leem `count = 0` e cada uma insere um `api_key_hash` **diferente** — o `UNIQUE` nunca colide, e `organizations` não tem constraint que sirva de ponto de serialização. Medido com quatro execuções em paralelo: **4 organizações criadas, 4 sucessos**, exatamente o que a spec dizia ser impossível |

A spec foi corrigida no mesmo commit, com uma subseção nova em "Modelo de dados" explicando por que a transação não basta. O mecanismo escolhido é o **terceiro** uso de advisory lock no projeto, pelo mesmo motivo dos outros dois — migrations no boot e reconciliador: quando a correção depende de "ninguém mais está fazendo isso agora", a promessa vem do banco e não da configuração.

Vale registrar como o furo foi encontrado: **pelo teste**, não por leitura. O `TestConcurrentBootstrapCreatesExactlyOneOrganization` entrou no plano durante a auto-revisão, justamente porque a spec listava esse modo de falha sem teste — e ele falhou contra a implementação que seguia a spec ao pé da letra. Sem esse teste, o bug teria ido para produção documentado como impossível.

## Divergências do plano

| Plano dizia | Ficou | Por quê |
|---|---|---|
| `sqlc.CreateOrganizationParams{ID: orgID}` com `orgID` de `ids.New()` | Helper `pgUUID(uuid.UUID) pgtype.UUID` em `bootstrap.go` | Os **nomes** que o plano previu saíram todos certos — `CountOrganizations`, `CreateOrganizationParams`, `CreateApplicationParams`, e `int64` no count. Os **tipos** não: as colunas `uuid` viram `pgtype.UUID` no gerado, e `ids.New()` devolve `uuid.UUID` |
| `fmt.Fprint(out, usage)` e cinco `fmt.Fprintf(out, …)` | `_, _ = fmt.Fprint(…)` | `errcheck` exclui `os.Stderr` da checagem por default, mas não um `io.Writer` genérico. Corrigido no plano ainda durante a execução, para a task seguinte não redescobrir |
| `if h.Hash(k) != h.Hash(k)` no teste de determinismo | Duas variáveis, comparadas | `staticcheck` recusa como `SA4000`, expressões idênticas nos dois lados do operador. A asserção é a mesma: um `Hash` com salt por chamada divergiria ali |

## Decisões que só apareceram implementando

**`internal/store` provavelmente devia expor a conversão `uuid.UUID` ↔ `pgtype.UUID`.** O helper `pgUUID` nasceu privado em `cmd/adminctl/bootstrap.go`, e a spec de ingress vai precisar do caminho inverso para ler `application_id` do banco. Duas cópias de uma conversão de três linhas não são um problema; três já são. Não promovi agora porque promover com um único consumidor é adivinhar a forma da interface — e §2 recusa isso.

**`flag.ContinueOnError` imprime o bloco de uso por conta própria**, no stderr, antes de o `main` imprimir o código de erro. A saída fica duplicada em caso de flag desconhecida. Correta em código e em exit status; feia. Não vale trocar por `flag.ContinueOnError` com `fs.SetOutput(io.Discard)` sem decidir primeiro se o uso deve aparecer — e isso é decisão de UX de CLI que ninguém pediu ainda.

Nenhuma das duas vale para todo o sistema, então nenhuma virou seção de `ARCHITECTURE.md`. A que valia — o formato e o hash da api key — já estava prevista e entrou como §4.33.

## Evidência de que funciona

**Testes** — 130 em 11 pacotes, dos quais 8 de integração com Postgres real:

```
ok  github.com/victorzix/vhook/cmd/adminctl        93.090s
--- PASS: TestBootstrapCreatesOneOrganizationAndOneApplication (3.82s)
--- PASS: TestStoredHashMatchesThePrintedKey (3.28s)
--- PASS: TestADifferentMasterKeyProducesADifferentStoredHash (6.52s)
--- PASS: TestSecondRunRefusesAndChangesNothing (13.42s)
--- PASS: TestInvalidFlagsFailBeforeTouchingTheDatabase (36.22s)  [4 subtestes]
--- PASS: TestAFailureBetweenTheInsertsLeavesNothingBehind (10.54s)
--- PASS: TestConcurrentBootstrapCreatesExactlyOneOrganization (5.11s)
--- PASS: TestDefaultsMatchTheSpec (14.11s)
```

Os que provam invariante, e não só ausência de erro:

- **O teste do pepper foi visto vermelho de propósito.** Trocando o HMAC por SHA-256 puro, `TestThePepperIsActuallyInTheComputation` falhou com "o pepper não está entrando no cálculo". Sem esse teste, uma implementação que ignorasse a chave mestra passaria em todos os outros — e o ganho inteiro de §4.33 estaria perdido sem nenhum sintoma em runtime.
- **O teste de completude do i18n acusou os dois códigos novos oito vezes**, duas por locale, antes das traduções entrarem. É a barreira criada na spec 001 pagando pela primeira vez em código que não é dela.
- **O hash gravado bate com `Hasher.Hash(chave impressa)`.** É o que prova que a chave impressa é utilizável; sem ele, o comando poderia imprimir uma chave e gravar o hash de outra, e só a spec de ingress descobriria — como um 401 em chave válida.
- **A transação foi provada derrubando `applications`:** o primeiro insert passa, o segundo falha, e `organizations` fica em zero. Nenhuma chave é impressa.
- **Distribuição do alfabeto sobre 10.000 chaves:** todos os 62 caracteres aparecem. Pega o viés de módulo que a amostragem por rejeição existe para evitar — uma implementação com `% 62` sobre bytes crus favoreceria os primeiros caracteres, e nenhum outro teste notaria.

**Manual** — dez cenários exercitados, todos os da tabela de comportamento observável e de modos de falha:

```
$ go run ./cmd/adminctl bootstrap
organization  org_01M130HV8ZEXASP8158FTTVP92  vhook
application   app_01M130HV8ZEXBBCKEBSBVWAZKK  default
              plan=free  locale=pt-BR  backoff_profile=production
api key       vhk_ZiJn0bcqlM1G8SyC7QDHdYbzz8W2Rbq4MepG7WV5ofO
              ^ shown once. It cannot be recovered.

$ go run ./cmd/adminctl bootstrap              → APP-CFL-001, exit 1
$ go run ./cmd/adminctl bootstrap --locale de  → APP-VAL-001, exit 1
   sem DATABASE_URL / sem MASTER_KEY / MASTER_KEY não base64 / curta → CFG-VAL-001, exit 1
   Postgres inalcançável → STO-DEP-001, exit 1
```

**Em nenhuma das dez saídas o valor de `VHOOK_MASTER_KEY` aparece** — só o nome da variável. Era invariante explícito da spec e tem teste dedicado.

O pepper provado de fora, apagando as linhas e trocando a chave mestra com a mesma flag `--app`:

```
hash com a chave mestra 1: 2b4611562c79ccbf702612add655704dd6ba62b24081da331bae1e51d8e7b839
hash com a chave mestra 2: 0146e952b944dc923d767bdff4b9e6a684fc6fd1a2ca9f584fab6c298b03ee28
```

E o id impresso fecha o caminho de volta pelo `psql`, o que valida a mitigação de §4.31 num uso real:

```sql
SELECT name, plan, locale, backoff_profile FROM applications
 WHERE id = vhook_id('app_01M130HV8ZEXBBCKEBSBVWAZKK');
  name   | plan | locale | backoff_profile
 default | free | pt-BR  | production
```

**Determinismo do código gerado** — regerar `sqlc` e `oapi-codegen` produz arquivos idênticos, verificado por hash. `git diff` não serviria de prova enquanto os gerados eram untracked.

## Contratos alterados

Nada. Esta release não expõe rota HTTP, e `contracts/openapi.yaml` não foi tocado. Os dois códigos de erro novos não exigem mudança de contrato porque o schema `Error` já é genérico.

## Pendente

| Item | Para onde foi |
|---|---|
| Rotação de api key | Roadmap da spec. O formato da chave já a suporta; falta a janela de duas chaves válidas |
| Rotação da chave mestra sem reemitir api key | **Dívida registrada em §4.33.** Hoje é impossível: recalcular o HMAC exige o valor em claro, que não guardamos |
| Checksum na chave | Roadmap. Retrocompatível: a validação passa a ser "se tem checksum, verifica" |
| Múltiplas applications por organização | Junto com plano pago |
| Últimos 4 caracteres da chave para exibição | Exige coluna nova, e não há tela ainda |
| Conversão `uuid.UUID` ↔ `pgtype.UUID` em `internal/store` | Promover quando a spec de ingress precisar do caminho inverso — com dois consumidores a forma da interface deixa de ser adivinhação |
| Saída duplicada de `flag.ContinueOnError` | Dívida aceita. Correta em código e exit; a decisão de UX de CLI não foi pedida |

**Nota operacional:** as linhas criadas no bootstrap manual foram apagadas do banco de desenvolvimento ao fim da verificação. A chave mestra usada nelas não existe em nenhum `.env`, então deixá-las faria o primeiro `bootstrap` de verdade recusar com `APP-CFL-001` por causa de uma credencial que ninguém tem.
