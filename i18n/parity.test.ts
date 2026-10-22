import { describe, expect, it } from "vitest";

import { errorCatalogs, locales } from "./index";

describe("catálogo de erros", () => {
  it("tem exatamente as mesmas chaves em todos os locales", () => {
    // Sem fallback em runtime é decisão de apps/CLAUDE.md: cair para `en`
    // quando falta `fr` esconde a lacuna, a tela funciona, e a chave fica
    // faltando por meses. Aqui a falta dói na hora.
    const reference = Object.keys(errorCatalogs["pt-BR"]).sort();

    for (const locale of locales) {
      const keys = Object.keys(errorCatalogs[locale]).sort();
      expect(keys, `locale ${locale} divergiu de pt-BR`).toEqual(reference);
    }
  });

  it("não tem mensagem vazia em nenhum locale", () => {
    for (const locale of locales) {
      for (const [code, message] of Object.entries(errorCatalogs[locale])) {
        expect(message.trim(), `${locale} / ${code}`).not.toBe("");
      }
    }
  });

  it("cobre o formato MOD-TYP-NNN em toda chave", () => {
    const format = /^[A-Z]{3}-[A-Z]{3}-\d{3}$/;
    for (const code of Object.keys(errorCatalogs["pt-BR"])) {
      expect(code, `chave fora do formato`).toMatch(format);
    }
  });
});
