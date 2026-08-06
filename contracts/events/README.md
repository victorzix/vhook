# Schemas de evento

JSON Schema de cada payload que o vhook **envia** para endpoints de cliente. Um arquivo por `event_type`:

```
events/
├── release.published.schema.json
├── endpoint.disabled.schema.json
└── delivery.failed.schema.json
```

Cada um é adicionado pela spec que introduz o evento.

## Estes schemas são contrato público

Quem integra com o vhook lê daqui para saber o que vai receber. Isso tem duas consequências:

**Campo novo é aditivo e opcional.** Remover campo ou torná-lo obrigatório quebra integração de cliente que já está em produção — e cliente de webhook não redeploya porque mudamos de ideia. Mudança incompatível exige `event_type` novo, não alteração no lugar.

**Mensagem de erro aqui vai resolvida.** Diferente da API (que manda só código), payload que sai para cliente carrega `code` **e** `message` no idioma de `applications.locale` — o sistema dele não tem o nosso catálogo. Ver [`docs/ERRORS.md`](../../docs/ERRORS.md).

## Campos que todo evento carrega

Vão nos headers, não no corpo, e por isso não aparecem nos schemas:

| Header | Conteúdo |
|---|---|
| `X-Vhook-Id` | id do evento, estável entre tentativas — é por ele que o cliente deduplica |
| `X-Vhook-Timestamp` | epoch em segundos, incluído no que é assinado |
| `X-Vhook-Signature` | `t=<ts>,v1=<hmac_sha256>` sobre `"{ts}.{corpo cru}"` |

A assinatura é sobre os **bytes crus** do corpo. Qualquer reserialização quebra a verificação do cliente.
