"use client";

import { useLocale, useTranslations } from "next-intl";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/features/endpoints/api/client";
import { useUpdateEndpointUrl } from "@/features/endpoints/hooks/use-endpoints";
import { messageForErrorCode } from "@/features/endpoints/lib/errors";
import { createEndpointSchema } from "@/features/endpoints/schemas/endpoint";

import type { Locale } from "@vhook/i18n";

type Props = {
  endpointId: string;
  url: string;
};

export function EndpointUrlEditor({ endpointId, url }: Props) {
  const t = useTranslations("endpoints");
  const locale = useLocale() as Locale;
  const update = useUpdateEndpointUrl();

  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  function cancel() {
    // Discard the draft, not just the input: keeping it would make the next
    // "edit" reopen on text the person already rejected.
    setDraft(null);
    setError(null);
  }

  async function save() {
    if (draft === null) return;

    // Same schema as creation: the rule about https is the endpoint's, not the
    // create form's, and duplicating it here is how the two drift apart.
    const parsed = createEndpointSchema.safeParse({ url: draft });
    if (!parsed.success) {
      setError(t("urlMustBeHttps"));
      return;
    }

    try {
      await update.mutateAsync({ id: endpointId, url: draft });
      cancel();
    } catch (caught) {
      const code = caught instanceof ApiError ? caught.code : null;
      setError((code && messageForErrorCode(code, locale)) || t("unknownError"));
    }
  }

  if (draft === null) {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-sm break-all">{url}</span>
        <Button type="button" variant="ghost" size="sm" onClick={() => setDraft(url)}>
          {t("edit")}
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <Input
        aria-label={t("urlLabel")}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        aria-invalid={Boolean(error)}
      />
      {error ? (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      ) : null}
      <div className="flex items-center gap-2">
        <Button type="button" size="sm" onClick={save} disabled={update.isPending}>
          {t("save")}
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={cancel}>
          {t("cancel")}
        </Button>
      </div>
    </div>
  );
}
