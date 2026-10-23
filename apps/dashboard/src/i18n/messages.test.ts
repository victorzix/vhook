import { describe, expect, it } from "vitest";

import en from "../../messages/en.json";
import es from "../../messages/es.json";
import fr from "../../messages/fr.json";
import ptBR from "../../messages/pt-BR.json";

/** Achata { a: { b: "x" } } em ["a.b"], para comparar árvores de tradução. */
function flatten(value: unknown, prefix = ""): string[] {
  if (typeof value !== "object" || value === null) return [prefix];
  return Object.entries(value).flatMap(([key, child]) =>
    flatten(child, prefix ? `${prefix}.${key}` : key),
  );
}

describe("mensagens de interface", () => {
  it("têm exatamente as mesmas chaves em todos os locales", () => {
    const reference = flatten(ptBR).sort();
    for (const [name, messages] of Object.entries({ en, es, fr })) {
      expect(flatten(messages).sort(), `locale ${name} divergiu`).toEqual(reference);
    }
  });
});
