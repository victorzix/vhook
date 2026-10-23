import { useFormatter, useTranslations } from "next-intl";

import { Badge } from "@/components/ui/badge";

import type { Endpoint } from "@/features/endpoints/api/client";

export function EndpointList({ endpoints }: { endpoints: Endpoint[] }) {
  const t = useTranslations("endpoints");
  const format = useFormatter();

  if (endpoints.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("empty")}</p>;
  }

  return (
    <ul className="flex flex-col gap-3">
      {endpoints.map((endpoint) => (
        <li key={endpoint.id} className="flex flex-col gap-1 rounded border p-3">
          <span className="font-mono text-sm break-all">{endpoint.url}</span>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Badge>{t(`status.${endpoint.status}`)}</Badge>
            <span>
              {t("createdAt")} {format.dateTime(new Date(endpoint.created_at))}
            </span>
          </div>
        </li>
      ))}
    </ul>
  );
}
