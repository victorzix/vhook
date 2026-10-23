import { describe, expect, it } from "vitest";

import { fieldForErrorCode, isPlanLimitError, messageForErrorCode } from "./errors";

describe("fieldForErrorCode", () => {
  it("aponta a URL para os três erros de validação de destino", () => {
    // Os três foram cunhados separados na spec 003 justamente porque
    // produzem mensagens diferentes: um código só forçaria a tela a adivinhar.
    for (const code of ["EPT-VAL-001", "EPT-VAL-002", "EPT-VAL-003"]) {
      expect(fieldForErrorCode(code)).toBe("url");
    }
  });

  it("aponta a URL para a duplicidade", () => {
    expect(fieldForErrorCode("EPT-CFL-001")).toBe("url");
  });

  it("não aponta campo para o limite do plano", () => {
    expect(fieldForErrorCode("RTL-LMT-001")).toBeNull();
  });

  it("não aponta campo para código desconhecido", () => {
    expect(fieldForErrorCode("XXX-YYY-999")).toBeNull();
  });
});

describe("isPlanLimitError", () => {
  it("reconhece o limite do plano", () => {
    expect(isPlanLimitError("RTL-LMT-001")).toBe(true);
    expect(isPlanLimitError("EPT-VAL-001")).toBe(false);
  });
});

describe("messageForErrorCode", () => {
  it("traduz pelo catálogo compartilhado com o Go", () => {
    const message = messageForErrorCode("EPT-VAL-001", "pt-BR");
    expect(message).not.toBe("");
    expect(message).not.toBe("EPT-VAL-001");
  });

  it("traduz o mesmo código de forma diferente em locales diferentes", () => {
    // Se estes forem iguais, o catálogo não está sendo consultado de verdade.
    expect(messageForErrorCode("EPT-VAL-001", "pt-BR")).not.toBe(
      messageForErrorCode("EPT-VAL-001", "fr"),
    );
  });

  it("devolve string vazia para código sem entrada, em vez de inventar texto", () => {
    // Chave ausente tem que doer: apps/CLAUDE.md recusa fallback de locale.
    expect(messageForErrorCode("XXX-YYY-999", "pt-BR")).toBe("");
  });
});
