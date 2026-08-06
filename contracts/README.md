# Contratos

Definição executável das duas superfícies do vhook. **Fonte única da verdade** — o código é gerado a partir daqui, nunca o contrário.

```
contracts/
├── openapi.yaml        API REST: ingress e management
└── events/*.schema.json  payloads que o vhook ENVIA para endpoints de cliente
```

## Por que dois formatos

OpenAPI descreve endpoints que **nós expomos**. Os payloads que o vhook entrega são requisições que **nós fazemos** contra o servidor de outra pessoa — não são operações nossas, e OpenAPI não modela isso bem. JSON Schema modela, e ainda serve como documentação consultável por quem integra.

## Contrato antes de código

Editar os contratos faz parte de **aprovar a spec**, não de implementá-la. É nesse momento que request, response e payload deixam de ser prosa e viram definição verificável, e é o que faz a implementação ser rápida: os tipos já existem quando o primeiro teste é escrito.

## Codegen

| Alvo | Ferramenta | Saída |
|---|---|---|
| Go | `oapi-codegen` | tipos e interface de servidor |
| Dashboard | `openapi-typescript` | tipos TS do cliente |

**Código gerado nunca é editado à mão.** Se a saída está errada, o contrato está errado — corrija o contrato e regenere. Editar o gerado funciona até a próxima geração e falha em silêncio no meio de outra tarefa.

Um teste verifica que o código gerado está em dia com o contrato. Sem ele, "esqueci de regenerar" vira divergência entre front e back que só aparece em runtime — exatamente o que ter contrato deveria impedir.

## Versionamento

`info.version` no `openapi.yaml` acompanha a versão do sistema, atualizada junto com a release.

Mudança incompatível em contrato já publicado exige caminho novo (`/v2/...`), não alteração no lugar. Cliente de webhook não redeploya porque nós mudamos de ideia.
