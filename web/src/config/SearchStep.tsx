import { useState, type ReactNode } from "react";
import type { SearchStatus } from "../api";
import { useIsAdmin, useSearchProviders, useSearchSites } from "../queries";
import { Button, Card, ErrorNote, Pending } from "../ui";
import { Connect, GoogleReturn, SitePicker } from "../pages/settings/SearchEngines";
import { siteName } from "../pages/search/model";

/**
 * Onboarding's search engines step: the Settings > Search engines
 * connections, each ending at a picked site. footer gets whether any engine
 * has a site, so it can offer Continue or Skip.
 */
export function SearchStep({ footer }: { footer: (picked: boolean) => ReactNode }) {
  const admin = useIsAdmin();
  const providers = useSearchProviders();
  if (!admin)
    return (
      <div className="space-y-5">
        <Card className="p-4 text-sm text-muted">
          Only an admin can connect a search engine. Skip this step, and an admin can connect one later in Settings &gt; Search engines.
        </Card>
        {footer(false)}
      </div>
    );
  return (
    <div className="space-y-5">
      <GoogleReturn s={providers.data?.find((s) => s.provider === "google")} />
      {providers.isPending ? (
        <Pending />
      ) : providers.isError ? (
        <ErrorNote error={providers.error} />
      ) : (
        providers.data.map((s) => (
          <section key={s.provider} aria-label={s.title} className="space-y-3">
            <h3 className="text-sm font-semibold">
              {s.title} <span className="font-normal text-muted">{s.product}</span>
            </h3>
            <Connection s={s} />
          </section>
        ))
      )}
      {footer(!!providers.data?.some((s) => s.connected && s.site))}
    </div>
  );
}

function Connection({ s }: { s: SearchStatus }) {
  const [picking, setPicking] = useState(false);
  useSearchSites(s.provider, s.connected);
  if (!s.connected)
    return (
      <Card className="p-4">
        <Connect s={s} returnTo="onboarding" />
      </Card>
    );
  if (!s.site || picking) return <SitePicker s={s} onDone={() => setPicking(false)} onCancel={s.site ? () => setPicking(false) : undefined} />;
  return (
    <Card className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
      <p className="min-w-0 flex-1 text-sm text-muted">
        Syncing <span className="font-medium text-fg [overflow-wrap:anywhere]">{siteName(s.site)}</span>
        {s.backfill_months === 0 ? `, with everything ${s.title} keeps.` : s.backfill ? `, with every day ${s.title} keeps.` : ", from the last 28 days."}
        {!s.last_sync_at && " The first sync starts within a minute."}
      </p>
      <Button onClick={() => setPicking(true)} className="self-start sm:self-auto">
        Change site
      </Button>
    </Card>
  );
}
