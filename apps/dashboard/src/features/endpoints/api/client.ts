import type { components } from "@/lib/api-types";

export type Endpoint = components["schemas"]["Endpoint"];
export type EndpointWithSecret = components["schemas"]["EndpointWithSecret"];

/** The error the BFF returns: code and correlation id, never a message. */
export class ApiError extends Error {
  constructor(
    readonly code: string,
    readonly correlationId: string,
  ) {
    // The message is the code on purpose: any text here would be text the
    // catalogue is supposed to own.
    super(code);
  }
}

async function parse<T>(response: Response): Promise<T> {
  const body = await response.json();
  if (!response.ok) {
    throw new ApiError(body?.error?.code ?? "SYS-INT-001", body?.error?.correlation_id ?? "");
  }
  return body as T;
}

export async function listEndpoints(): Promise<Endpoint[]> {
  const body = await parse<{ endpoints: Endpoint[] }>(await fetch("/api/endpoints"));
  return body.endpoints;
}

export async function createEndpoint(url: string): Promise<EndpointWithSecret> {
  return parse<EndpointWithSecret>(
    await fetch("/api/endpoints", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url }),
    }),
  );
}

export async function updateEndpointUrl(id: string, url: string): Promise<Endpoint> {
  return parse<Endpoint>(
    await fetch(`/api/endpoints/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url }),
    }),
  );
}
