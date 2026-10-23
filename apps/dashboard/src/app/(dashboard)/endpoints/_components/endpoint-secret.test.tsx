import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it } from "vitest";

import messages from "../../../../../messages/pt-BR.json";
import { EndpointSecret } from "./endpoint-secret";

const secret = "whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H";

function renderSecret() {
  return render(
    <NextIntlClientProvider locale="pt-BR" messages={messages}>
      <EndpointSecret secret={secret} />
    </NextIntlClientProvider>,
  );
}

describe("EndpointSecret", () => {
  it("começa mascarado", () => {
    renderSecret();
    expect(screen.queryByText(secret)).not.toBeInTheDocument();
  });

  it("revela o valor completo ao pedir", async () => {
    renderSecret();
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.reveal }));
    expect(screen.getByText(secret)).toBeInTheDocument();
  });

  it("volta a esconder", async () => {
    renderSecret();
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.reveal }));
    await userEvent.click(screen.getByRole("button", { name: messages.endpoints.hide }));
    expect(screen.queryByText(secret)).not.toBeInTheDocument();
  });
});
