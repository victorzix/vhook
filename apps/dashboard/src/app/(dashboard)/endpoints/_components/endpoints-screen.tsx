"use client";

import { useTranslations } from "next-intl";
import { useState } from "react";

import { useEndpoints } from "@/features/endpoints/hooks/use-endpoints";

import { EndpointForm } from "./endpoint-form";
import { EndpointList } from "./endpoint-list";
import { EndpointSecret } from "./endpoint-secret";

import type { EndpointWithSecret } from "@/features/endpoints/api/client";

// §4.28: 2 endpoints por application no plano free.
const PLAN_LIMIT = 2;

export function EndpointsScreen() {
  const t = useTranslations("endpoints");
  const { data: endpoints = [] } = useEndpoints();
  const [created, setCreated] = useState<EndpointWithSecret | null>(null);

  return (
    <main className="mx-auto flex max-w-2xl flex-col gap-6 p-4">
      <h1 className="text-2xl font-semibold">{t("title")}</h1>

      <EndpointForm
        isAtPlanLimit={endpoints.length >= PLAN_LIMIT}
        onEndpointCreated={setCreated}
      />

      {created ? (
        <section className="rounded border p-3">
          <h2 className="mb-2 text-sm font-medium">{t("secretLabel")}</h2>
          <EndpointSecret secret={created.secret} />
        </section>
      ) : null}

      <EndpointList endpoints={endpoints} />
    </main>
  );
}
