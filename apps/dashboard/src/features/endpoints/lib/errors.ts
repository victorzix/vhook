import { errorCatalogs, type Locale } from "@vhook/i18n";

/** Codes whose failure belongs to the URL field of the form. */
const URL_FIELD_CODES = new Set([
  "EPT-VAL-001",
  "EPT-VAL-002",
  "EPT-VAL-003",
  "EPT-CFL-001",
]);

const PLAN_LIMIT_CODE = "RTL-LMT-001";

/** Which form field to mark, or null when the failure is not about a field. */
export function fieldForErrorCode(code: string): "url" | null {
  return URL_FIELD_CODES.has(code) ? "url" : null;
}

export function isPlanLimitError(code: string): boolean {
  return code === PLAN_LIMIT_CODE;
}

/**
 * Translates through the same catalogue the Go side embeds — one file, one
 * text. Returns "" for an unknown code instead of inventing one: a missing key
 * has to hurt, which is why there is no runtime locale fallback.
 */
export function messageForErrorCode(code: string, locale: Locale): string {
  const catalogue = errorCatalogs[locale] as Record<string, string>;
  return catalogue[code] ?? "";
}
