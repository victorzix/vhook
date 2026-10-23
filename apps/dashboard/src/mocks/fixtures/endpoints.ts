import type { components } from "@/lib/api-types";

// Tipados pelo contrato de propósito: se contracts/openapi.yaml mudar, o build
// quebra AQUI. É o que separa "mock que reflete a API" de "mock que refletia
// a API há três semanas".
type EndpointWithSecret = components["schemas"]["EndpointWithSecret"];

export const seedEndpoints: EndpointWithSecret[] = [
  {
    id: "ept_01J4PMX3R0E008000000000003",
    url: "https://api.acme.com/webhooks/vhook",
    status: "active",
    created_at: "2026-08-20T14:03:11Z",
    secret: "whsec_zDccFjpqVDQHpyWI9SskzezueMASw60LLuaLOFjmD8H",
  },
];

/**
 * Hosts que o mock recusa, para os caminhos de erro serem exercitáveis à mão.
 * Sem isto o tratamento de erro é escrito e nunca visto funcionando.
 */
export const forbiddenHosts = ["10.0.0.1", "169.254.169.254", "localhost"];

/** §4.28: o plano free permite 2 endpoints por application. */
export const planLimit = 2;
