import { getRequestConfig } from "next-intl/server";

import { defaultLocale } from "@vhook/i18n";

// Locale fixo nesta release: não há sessão nem seletor. Quando o login existir,
// ele vem de lá — e é a única linha que muda.
export default getRequestConfig(async () => ({
  locale: defaultLocale,
  messages: (await import(`../../messages/${defaultLocale}.json`)).default,
}));
