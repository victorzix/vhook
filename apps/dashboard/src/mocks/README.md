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
