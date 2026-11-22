import { useState, type ReactNode } from "react";
import type { GscStatus } from "../api";
import { useGsc, useGscProperties, useIsAdmin } from "../queries";
import { Button, Card, ErrorNote, Pending } from "../ui";
import { GoogleButton, GoogleReturn, PropertyPicker, ServiceAccountForm } from "../pages/settings/SearchConsole";

/**
 * Onboarding step 5: the Settings > Search Console connection, ending at a
 * picked property. Picking one saves the step; footer gets whether one is
 * picked, so it can offer Continue or Skip.
 */
export function SearchConsoleStep({ onPicked, footer }: { onPicked: () => void; footer: (picked: boolean) => ReactNode }) {
  const admin = useIsAdmin();
  const gsc = useGsc();
  if (!admin)
    return (
      <div className="space-y-5">
        <Card className="p-4 text-sm text-muted">
          Only an admin can connect Search Console. Skip this step, and an admin can connect it later in Settings &gt; Search Console.
        </Card>
        {footer(false)}
      </div>
    );
  return (
    <div className="space-y-5">
      <GoogleReturn s={gsc.data} />
      {gsc.isPending ? <Pending /> : gsc.isError ? <ErrorNote error={gsc.error} /> : <Connection s={gsc.data} onPicked={onPicked} />}
      {footer(!!gsc.data?.connected && !!gsc.data.property)}
    </div>
  );
}

function Connection({ s, onPicked }: { s: GscStatus; onPicked: () => void }) {
  const [picking, setPicking] = useState(false);
  useGscProperties(s.connected);
  if (!s.connected && !s.oauth_available)
    return (
      <Card className="space-y-4 p-4">
        <div>
          <h3 className="mb-1 text-sm font-semibold">Connect with a Google service account</h3>
          <ol className="list-decimal space-y-1 pl-5 text-sm text-muted">
            <li>In Google Cloud, create a service account and download a JSON key for it.</li>
            <li>In Search Console, add the service account&apos;s email as a user on your site. Restricted access is enough.</li>
            <li>Paste the whole key file below.</li>
          </ol>
        </div>
        <ServiceAccountForm />
        <p className="border-t border-line pt-3 text-xs text-muted">
          Not set up with Google Cloud? Skip this step. Whoever runs your Google account can connect it later in Settings &gt; Search Console.
        </p>
      </Card>
    );
  if (!s.connected)
    return (
      <Card className="space-y-5 p-4">
        <div>
          <h3 className="mb-1 text-sm font-semibold">Sign in with Google</h3>
          <p className="mb-3 text-sm text-muted">Byname asks for read-only access to Search Console and nothing else. Use an account that can see your property.</p>
          <GoogleButton available={s.oauth_available} primary returnTo="onboarding" />
        </div>
        <details className="border-t border-line pt-4 text-sm">
          <summary className="cursor-pointer py-3.5 font-medium text-muted hover:text-fg md:py-0">Or use a service account</summary>
          <div className="mt-3">
            <ServiceAccountForm />
          </div>
        </details>
      </Card>
    );
  if (!s.property || picking)
    return (
      <section>
        <h3 className="mb-3 text-sm font-semibold">{s.property ? "Change property" : "Pick a property"}</h3>
        <PropertyPicker s={s} onDone={onPicked} onCancel={s.property ? () => setPicking(false) : undefined} submitLabel="Save and continue" />
      </section>
    );
  return (
    <Card className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
      <p className="min-w-0 flex-1 text-sm text-muted">
        Syncing <span className="font-medium text-fg [overflow-wrap:anywhere]">{s.property}</span>
        {s.backfill ? ", with every day Google keeps." : ", from the last 28 days."} The first sync starts within a minute.
      </p>
      <Button onClick={() => setPicking(true)} className="self-start sm:self-auto">
        Change property
      </Button>
    </Card>
  );
}
