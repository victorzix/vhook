import { NextResponse } from "next/server";

import { mockStore } from "@/mocks/store";

import { errorEnvelope } from "../route";

export async function GET(_request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const result = mockStore.get(id);
  if (typeof result === "string") return errorEnvelope(result);
  return NextResponse.json(result);
}

export async function PATCH(request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const body = (await request.json()) as { url?: unknown };
  if (typeof body.url !== "string") return errorEnvelope("EPT-VAL-001");

  const result = mockStore.updateUrl(id, body.url);
  if (typeof result === "string") return errorEnvelope(result);
  return NextResponse.json(result);
}
