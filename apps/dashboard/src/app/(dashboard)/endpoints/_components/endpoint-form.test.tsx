import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";

import { errorCatalogs } from "@vhook/i18n";

import messages from "../../../../../messages/pt-BR.json";
import { EndpointForm } from "./endpoint-form";

afterEach(() => {
  vi.restoreAllMocks();
});

function renderForm(props: Partial<Parameters<typeof EndpointForm>[0]> = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <NextIntlClientProvider locale="pt-BR" messages={messages}>
      <QueryClientProvider client={client}>
        <EndpointForm isAtPlanLimit={false} onEndpointCreated={vi.fn()} {...props} />
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

describe("EndpointForm", () => {
  it("recusa http antes de chamar o BFF", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderForm();

    await userEvent.type(screen.getByLabelText(messages.endpoints.urlLabel), "http://acme.com/x");
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.create }));

    expect(await screen.findByText(messages.endpoints.urlMustBeHttps)).toBeInTheDocument();
    // A validação local é o que evita a ida ao servidor. Se o fetch aconteceu,
    // ela não está no lugar certo.
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("mostra o texto do catálogo quando o BFF recusa, sem prefixo de chave", async () => {
    // O campo recebe dois tipos de mensagem: chave, quando quem reprova é o
    // zod, e frase já resolvida, quando quem reprova é a API. Traduzir as duas
    // transforma a segunda em `endpoints.<frase inteira>` e enche o console de
    // MISSING_MESSAGE — sem quebrar nada, que é o que faz passar despercebido.
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "EPT-VAL-002", correlation_id: "abc" } }), {
        status: 422,
        headers: { "Content-Type": "application/json" },
      }),
    );
    renderForm();

    await userEvent.type(screen.getByLabelText(messages.endpoints.urlLabel), "https://10.0.0.1/x");
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.create }));

    const fromCatalogue = errorCatalogs["pt-BR"]["EPT-VAL-002"];
    expect(await screen.findByText(fromCatalogue)).toBeInTheDocument();
  });

  it("desabilita o cadastro quando o plano está no limite", () => {
    renderForm({ isAtPlanLimit: true });
    expect(screen.getByRole("button", { name: messages.endpoints.create })).toBeDisabled();
  });

  it("diz por que o cadastro está desabilitado", () => {
    // Botão cinza sem motivo é a interface mandando a pessoa adivinhar. A spec
    // pede a mensagem de RTL-LMT-001, e ela vem do catálogo como qualquer
    // outra — o limite é do plano, não um detalhe desta tela.
    renderForm({ isAtPlanLimit: true });
    expect(screen.getByText(errorCatalogs["pt-BR"]["RTL-LMT-001"])).toBeInTheDocument();
  });
});
