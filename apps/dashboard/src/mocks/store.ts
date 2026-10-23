import type { components } from "@/lib/api-types";

import { forbiddenHosts, planLimit, seedEndpoints } from "./fixtures/endpoints";

type EndpointWithSecret = components["schemas"]["EndpointWithSecret"];

/** O código de erro que o BFF deve devolver, ou null quando pode prosseguir. */
export type MockRejection =
  | "EPT-VAL-001"
  | "EPT-VAL-002"
  | "EPT-CFL-001"
  | "RTL-LMT-001"
  | "EPT-NFD-001";

// Estado de módulo: reinicia com o servidor e é por instância. Irrelevante
// agora, e some junto com a pasta.
let endpoints: EndpointWithSecret[] = [...seedEndpoints];

function randomSecret(): string {
  const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
  let body = "";
  for (let i = 0; i < 43; i += 1) {
    body += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return `whsec_${body}`;
}

function randomId(): string {
  const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
  let body = "";
  for (let i = 0; i < 26; i += 1) {
    body += crockford[Math.floor(Math.random() * crockford.length)];
  }
  return `ept_${body}`;
}

function reject(url: string, ignoreId?: string): MockRejection | null {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return "EPT-VAL-001";
  }
  if (parsed.protocol !== "https:") return "EPT-VAL-001";
  if (forbiddenHosts.includes(parsed.hostname)) return "EPT-VAL-002";
  if (endpoints.some((e) => e.url === url && e.id !== ignoreId)) return "EPT-CFL-001";
  return null;
}

export const mockStore = {
  /** A listagem NUNCA carrega o secret — §4.28 e a spec 003. */
  list(): components["schemas"]["Endpoint"][] {
    return endpoints.map(({ secret: _secret, ...rest }) => rest);
  },

  get(id: string): EndpointWithSecret | MockRejection {
    return endpoints.find((e) => e.id === id) ?? "EPT-NFD-001";
  },

  create(url: string): EndpointWithSecret | MockRejection {
    if (endpoints.length >= planLimit) return "RTL-LMT-001";
    const rejection = reject(url);
    if (rejection) return rejection;

    const created: EndpointWithSecret = {
      id: randomId(),
      url,
      status: "active",
      created_at: new Date().toISOString(),
      secret: randomSecret(),
    };
    endpoints = [...endpoints, created];
    return created;
  },

  updateUrl(id: string, url: string): components["schemas"]["Endpoint"] | MockRejection {
    const existing = endpoints.find((e) => e.id === id);
    if (!existing) return "EPT-NFD-001";
    const rejection = reject(url, id);
    if (rejection) return rejection;

    // O secret sobrevive ao PATCH: é o motivo de o PATCH existir.
    const updated = { ...existing, url };
    endpoints = endpoints.map((e) => (e.id === id ? updated : e));
    const { secret: _secret, ...withoutSecret } = updated;
    return withoutSecret;
  },
};
