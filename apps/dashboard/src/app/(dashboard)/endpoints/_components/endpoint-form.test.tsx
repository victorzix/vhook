import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it, vi } from "vitest";

import messages from "../../../../../messages/pt-BR.json";
import { EndpointForm } from "./endpoint-form";

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

  it("desabilita o cadastro quando o plano está no limite", () => {
    renderForm({ isAtPlanLimit: true });
    expect(screen.getByRole("button", { name: messages.endpoints.create })).toBeDisabled();
  });
});
