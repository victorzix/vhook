"use client";

import { Eye, EyeOff } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { maskSecret } from "@/features/endpoints/lib/secret";

export function EndpointSecret({ secret }: { secret: string }) {
  // Masked by default: this dashboard gets shown to people, and a secret on
  // screen leaks into screenshots, screen shares and recordings.
  const [isRevealed, setIsRevealed] = useState(false);
  const t = useTranslations("endpoints");

  return (
    <div className="flex items-center gap-2">
      <code className="font-mono text-sm">{isRevealed ? secret : maskSecret(secret)}</code>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => setIsRevealed((current) => !current)}
      >
        {isRevealed ? <EyeOff aria-hidden /> : <Eye aria-hidden />}
        {isRevealed ? t("hide") : t("reveal")}
      </Button>
    </div>
  );
}
