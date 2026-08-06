# NNN — <Nome da feature> · Plano

**O formato deste documento é definido pela skill `writing-plans`. Invoque-a em vez de copiar a estrutura daqui.** Ela define o header obrigatório, o bloco de cada task (Files, Interfaces, Steps) e a granularidade de 2-5 minutos por passo. Duplicar isso aqui criaria duas fontes que divergem.

Este arquivo registra só o que é específico do vhook e que a skill não pode saber.

---

## Overrides deste projeto

- **Salvar em** `docs/specs/NNN-name/plan.md` — não em `docs/plans/` nem em `docs/superpowers/plans/`.
- **Uma spec, um plano, uma release.** Cada plano termina com um `v0.X.0` demonstrável.
- **Commits em Conventional Commits**, com o `feat:` do passo final sendo o que gera a release.

## Global Constraints — herdadas por toda task

Copiar literalmente para a seção `Global Constraints` do plano. Toda task herda isso sem repetir:

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

## Antes de encerrar o plano

- [ ] Toda seção da spec tem uma task que a implementa
- [ ] Nenhum placeholder (`TBD`, "tratar erros", "escrever testes para o acima")
- [ ] Nomes de função e tipos consistentes entre tasks
- [ ] Os testes de integração dos invariantes tocados estão em alguma task
- [ ] A última task deixa a feature demonstrável pelo `sink` ou pelo playground
