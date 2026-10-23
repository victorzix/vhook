import { describe, expect, it } from "vitest";

import { createEndpointSchema } from "./endpoint";

describe("createEndpointSchema", () => {
  it("aceita https", () => {
    expect(createEndpointSchema.safeParse({ url: "https://acme.com/hooks" }).success).toBe(true);
  });

  it("recusa http antes de chegar ao BFF", () => {
    // Validar aqui é o que faz o erro aparecer sem ida ao servidor. A garantia
    // continua sendo do back — isto é conveniência, não segurança.
    expect(createEndpointSchema.safeParse({ url: "http://acme.com/hooks" }).success).toBe(false);
  });

  it("recusa texto que não é URL", () => {
    expect(createEndpointSchema.safeParse({ url: "não é uma url" }).success).toBe(false);
  });

  it("recusa vazio", () => {
    expect(createEndpointSchema.safeParse({ url: "" }).success).toBe(false);
  });
});
