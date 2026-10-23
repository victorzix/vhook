"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useLocale, useTranslations } from "next-intl";
import { useForm } from "react-hook-form";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ApiError, type EndpointWithSecret } from "@/features/endpoints/api/client";
import { useCreateEndpoint } from "@/features/endpoints/hooks/use-endpoints";
import {
  fieldForErrorCode,
  messageForErrorCode,
} from "@/features/endpoints/lib/errors";
import {
  createEndpointSchema,
  type CreateEndpointInput,
} from "@/features/endpoints/schemas/endpoint";

import type { Locale } from "@vhook/i18n";

const PLAN_LIMIT_CODE = "RTL-LMT-001";

type Props = {
  isAtPlanLimit: boolean;
  onEndpointCreated: (endpoint: EndpointWithSecret) => void;
};

export function EndpointForm({ isAtPlanLimit, onEndpointCreated }: Props) {
  const t = useTranslations("endpoints");
  const locale = useLocale() as Locale;
  const create = useCreateEndpoint();

  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors },
  } = useForm<CreateEndpointInput>({ resolver: zodResolver(createEndpointSchema) });

  async function onSubmit(input: CreateEndpointInput) {
    try {
      onEndpointCreated(await create.mutateAsync(input.url));
      reset();
    } catch (error) {
      if (!(error instanceof ApiError)) {
        setError("root", { message: t("unknownError") });
        return;
      }
      const field = fieldForErrorCode(error.code);
      // The catalogue owns the text; this component only decides where it goes.
      const message = messageForErrorCode(error.code, locale) || t("unknownError");
      setError(field ?? "root", { message });
    }
  }

  // The field receives two kinds of message. Zod sets a KEY, so the schema
  // stays free of copy and gets translated here. The catch above sets text the
  // shared catalogue already resolved — translating that yields
  // `endpoints.<whole sentence>` and a MISSING_MESSAGE in the console, which
  // renders without crashing and is exactly why it slips through.
  const rawUrlError = errors.url?.message;
  const urlError = rawUrlError ? (t.has(rawUrlError) ? t(rawUrlError) : rawUrlError) : undefined;

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-3">
      <label htmlFor="url" className="text-sm font-medium">
        {t("urlLabel")}
      </label>
      <Input
        id="url"
        {...register("url")}
        placeholder={t("urlPlaceholder")}
        aria-invalid={Boolean(urlError)}
        aria-describedby={urlError ? "url-error" : undefined}
      />
      {urlError ? (
        <p id="url-error" role="alert" className="text-sm text-destructive">
          {urlError}
        </p>
      ) : null}
      {errors.root?.message ? (
        <p role="alert" className="text-sm text-destructive">
          {errors.root.message}
        </p>
      ) : null}
      {isAtPlanLimit ? (
        // Botão cinza sem motivo faz a pessoa adivinhar. O texto é do
        // catálogo: o limite é do plano, não uma regra desta tela.
        <p className="text-sm text-muted-foreground">
          {messageForErrorCode(PLAN_LIMIT_CODE, locale)}
        </p>
      ) : null}
      <Button type="submit" disabled={isAtPlanLimit || create.isPending}>
        {create.isPending ? t("creating") : t("create")}
      </Button>
    </form>
  );
}
