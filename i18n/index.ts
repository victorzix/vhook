// The error catalogue is shared between Go and TypeScript by design: go:embed
// on one side, this import on the other. One file, one text — see §4.29.
import en from "./errors.en.json";
import es from "./errors.es.json";
import fr from "./errors.fr.json";
import ptBR from "./errors.pt-BR.json";

export const errorCatalogs = {
  "pt-BR": ptBR,
  en,
  es,
  fr,
} as const;

export type Locale = keyof typeof errorCatalogs;

export const locales = Object.keys(errorCatalogs) as Locale[];

/** The locale of applications.locale by default, and the fallback nowhere. */
export const defaultLocale: Locale = "pt-BR";
