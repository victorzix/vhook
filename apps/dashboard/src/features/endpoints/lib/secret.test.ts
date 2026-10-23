import { describe, expect, it } from "vitest";

import { maskSecret } from "./secret";

const real = "whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H";

describe("maskSecret", () => {
  it("mantém o prefixo, para o valor continuar reconhecível", () => {
    expect(maskSecret(real).startsWith("whsec_")).toBe(true);
  });

  it("mantém os últimos quatro caracteres, para conferência visual", () => {
    expect(maskSecret(real).endsWith("mD8H")).toBe(true);
  });

  it("esconde o miolo", () => {
    // O dashboard é mostrado para gente: secret em tela vaza em screenshot,
    // em compartilhamento de tela e em gravação.
    expect(maskSecret(real)).not.toContain("zDccFjpqVDQ");
  });

  it("não devolve string vazia para entrada válida", () => {
    expect(maskSecret(real).length).toBeGreaterThan(10);
  });

  it("não quebra com entrada curta ou vazia", () => {
    expect(() => maskSecret("")).not.toThrow();
    expect(() => maskSecret("whsec_ab")).not.toThrow();
    expect(maskSecret("whsec_ab")).not.toContain("ab");
  });
});
