"use client";

import { Check, Copy, Eye, EyeOff } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { maskSecret } from "@/features/endpoints/lib/secret";

export function EndpointSecret({ secret }: { secret: string }) {
  // Masked by default: this dashboard gets shown to people, and a secret on
  // screen leaks into screenshots, screen shares and recordings.
  const [isRevealed, setIsRevealed] = useState(false);
  const [hasCopied, setHasCopied] = useState(false);
  const t = useTranslations("endpoints");

  async function copy() {
    // The full value, never what is on screen — copying the mask would copy
    // the bullets, and the secret is only useful whole.
    await navigator.clipboard.writeText(secret);
    setHasCopied(true);
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <code className="font-mono text-sm break-all">
        {isRevealed ? secret : maskSecret(secret)}
      </code>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => setIsRevealed((current) => !current)}
      >
        {isRevealed ? <EyeOff aria-hidden /> : <Eye aria-hidden />}
        {isRevealed ? t("hide") : t("reveal")}
      </Button>
      <Button type="button" variant="ghost" size="sm" onClick={copy}>
        {hasCopied ? <Check aria-hidden /> : <Copy aria-hidden />}
        {hasCopied ? t("copied") : t("copy")}
      </Button>
    </div>
  );
}
