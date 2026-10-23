import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
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
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <NextIntlClientProvider locale="pt-BR" messages={messages}>
      <QueryClientProvider client={client}>
        <EndpointList endpoints={items} />
      </QueryClientProvider>
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
