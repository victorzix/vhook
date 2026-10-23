import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";

import messages from "../../../../../messages/pt-BR.json";
import { EndpointUrlEditor } from "./endpoint-url-editor";

const id = "ept_01J4PMX3R0E008000000000003";
const url = "https://api.acme.com/webhooks";
const corrected = "https://api.acme.com/webhooks/v2";

afterEach(() => {
  vi.restoreAllMocks();
});

function renderEditor() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <NextIntlClientProvider locale="pt-BR" messages={messages}>
      <QueryClientProvider client={client}>
        <EndpointUrlEditor endpointId={id} url={url} />
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

describe("EndpointUrlEditor", () => {
  it("mostra a URL sem campo de edição até pedirem", () => {
    renderEditor();
    expect(screen.getByText(url)).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("manda PATCH só para o endpoint editado", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({ id, url: corrected, status: "active", created_at: "2026-08-20T14:03:11Z" }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    renderEditor();
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.edit }));
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.type(screen.getByRole("textbox"), corrected);
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.save }));

    expect(fetchSpy).toHaveBeenCalledWith(
      `/api/endpoints/${id}`,
      expect.objectContaining({ method: "PATCH", body: JSON.stringify({ url: corrected }) }),
    );
  });

  it("desiste sem chamar o BFF ao cancelar", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");

    renderEditor();
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.edit }));
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.type(screen.getByRole("textbox"), corrected);
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.cancel }));

    expect(fetchSpy).not.toHaveBeenCalled();
    // Cancelar que deixa o texto digitado em tela não cancelou nada.
    expect(screen.getByText(url)).toBeInTheDocument();
  });
});
