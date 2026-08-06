# Catálogo de erros

Contrato de erro do vhook. A decisão e o raciocínio estão em [`ARCHITECTURE.md` §4.29](ARCHITECTURE.md).

## Formato

```
MOD-TYP-NNN
 │   │   └── sequencial de 3 dígitos, dentro do par módulo+tipo
 │   └────── tipo: a classe da falha
 └────────── módulo: onde a falha aconteceu
```

Toda constante carrega **código, nível e status HTTP**. Nenhuma carrega texto.

## Módulos

| Código | Área |
|---|---|
| `ING` | Ingress — recebimento de evento |
| `EPT` | Endpoints — cadastro e ciclo de vida |
| `DLV` | Delivery — disparo, retry, DLQ |
| `QUE` | Fila — publish, consumo, topologia |
| `AUT` | Autenticação — api key, token administrativo |
| `APP` | Applications e tenancy |
| `STO` | Persistência |
| `RTL` | Rate limit e quota de plano |
| `CFG` | Configuração |
| `SYS` | Interno e inesperado |

## Tipos

O tipo define o nível e o status HTTP padrão. A constante individual pode sobrescrever quando houver motivo — corpo malformado, por exemplo, é `VAL` mas responde 400 em vez de 422.

| Código | Classe | Nível | Status |
|---|---|---|---|
| `VAL` | Valor inválido ou fora do contrato | `warn` | 422 |
| `CRD` | Credencial ausente ou inválida | `warn` | 401 |
| `PRM` | Sem permissão para o recurso | `warn` | 403 |
| `NFD` | Recurso não encontrado | `warn` | 404 |
| `CFL` | Conflito — idempotência, duplicidade | `warn` | 409 |
| `LMT` | Limite excedido — rate, quota de plano | `warn` | 429 |
| `DEP` | Dependência indisponível | `error` | 502 |
| `TMO` | Timeout | `error` | 504 |
| `INT` | Falha interna não prevista | `error` | 500 |

Os tipos de `VAL` a `LMT` são `warn` porque são resultado esperado de entrada do cliente — o sistema funcionou. `DEP`, `TMO` e `INT` são `error` porque indicam algo do nosso lado.

## As três superfícies

### API → dashboard

Sem mensagem. O front resolve o código pelo catálogo i18n.

```json
{
  "error": {
    "code": "EPT-VAL-001",
    "correlation_id": "01HQZX3K7YB2N4M8P6R9T5V0W1",
    "details": [
      { "field": "url", "code": "EPT-VAL-003" }
    ]
  }
}
```

`details` existe para o dashboard destacar o campo errado sem que o backend produza texto. `correlation_id` é obrigatório em toda resposta de erro: sem mensagem, é a única forma de ligar um caso relatado ao log.

### vhook → endpoint do cliente

Mensagem já resolvida, no idioma de `applications.locale`. O sistema do cliente não tem o nosso catálogo.

```json
{
  "event_type": "endpoint.disabled",
  "error": {
    "code": "DLV-DEP-004",
    "message": "Endpoint desativado após 5 falhas consecutivas."
  }
}
```

### Log e métrica

Código, nível e todo o detalhe técnico — incluindo o que nunca sai numa resposta. Sujeito à regra de nunca logar payload nem header de assinatura.

## Onde cada coisa vive

| Artefato | Caminho | Contém |
|---|---|---|
| Registro | `internal/errs/` | código, nível, status. **Nenhum texto.** |
| Catálogo | `i18n/errors.<locale>.json` | código → mensagem. **Nenhum comportamento.** |

A v1 tem um locale só: `i18n/errors.pt-BR.json`, que é também o default de `applications.locale`. Idioma novo é um arquivo novo, sem migração — e o teste de completude abaixo é o que garante que ele entre inteiro ou não entre.

Separados de propósito: é o que impede as duas fontes de divergirem. O catálogo é consumido pelos dois lados — `go:embed` no Go, import direto no dashboard — então existe uma cópia só.

Um teste garante que todo código do registro tem entrada em todo locale. Código sem mensagem passa em review e aparece em produção como string vazia.

## Propagação entre serviços

**O código de origem atravessa os saltos.** Um serviço que recebe erro de outro repassa o código original e não o recodifica — recodificar a cada camada perde justamente a informação que se quer quando algo falha três níveis abaixo.

Código novo só quando a semântica muda de verdade. Nesse caso o original vai para o log, nunca é descartado.

## Ao adicionar um erro

1. Constante em `internal/errs/`, com nível e status vindos do tipo salvo motivo explícito.
2. Entrada em **todos** os arquivos de locale.
3. Se for um erro que sai para endpoint de cliente, revisar se a mensagem não expõe detalhe interno.
4. Nunca `fmt.Errorf` com texto livre atravessando fronteira de serviço ou de API. Texto livre dentro de um pacote, envolvido antes de sair, é aceitável.
