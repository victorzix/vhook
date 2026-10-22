import { describe, expect, it } from "vitest";

import { defaultLocale, errorCatalogs, locales } from "@vhook/i18n";

describe("catálogo compartilhado visto de dentro do app", () => {
  // Não duplica o teste de paridade do pacote: o que se prova aqui é a costura
  // do workspace — dependência declarada, `transpilePackages` e resolução de
  // JSON. Se qualquer uma cair, o front perde a tradução de erro inteira, e
  // sem este teste a descoberta é em tela.
  it("resolve @vhook/i18n a partir do dashboard", () => {
    expect(locales).toEqual(["pt-BR", "en", "es", "fr"]);
    expect(defaultLocale).toBe("pt-BR");
  });

  it("traz o texto dos códigos, não só as chaves", () => {
    expect(errorCatalogs[defaultLocale]["EPT-VAL-002"]).toContain("rede");
  });
});
