import { NextResponse } from "next/server";

import { mockStore, type MockRejection } from "@/mocks/store";

/** Status de cada código, espelhando o que a API Go devolve (spec 003). */
const statusFor: Record<MockRejection, number> = {
  "EPT-VAL-001": 422,
  "EPT-VAL-002": 422,
  "EPT-CFL-001": 409,
  "RTL-LMT-001": 403,
  "EPT-NFD-001": 404,
};

/**
 * O envelope de erro do projeto: código, correlation id e details. NUNCA
 * mensagem — o front traduz o código pelo catálogo (§4.29).
 */
export function errorEnvelope(code: MockRejection) {
  return NextResponse.json(
    {
      error: {
        code,
        correlation_id: crypto.randomUUID(),
        details: code.startsWith("EPT-VAL") ? [{ field: "url", code }] : undefined,
      },
    },
    { status: statusFor[code] },
  );
}

export async function GET() {
  return NextResponse.json({ endpoints: mockStore.list() });
}

export async function POST(request: Request) {
  const body = (await request.json()) as { url?: unknown };
  if (typeof body.url !== "string") return errorEnvelope("EPT-VAL-001");

  const result = mockStore.create(body.url);
  if (typeof result === "string") return errorEnvelope(result);
  return NextResponse.json(result, { status: 201 });
}
