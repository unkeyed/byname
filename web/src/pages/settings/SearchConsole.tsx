import { useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { api, type GscProperty, type GscStatus } from "../../api";
import { keys, useGsc, useGscProperties } from "../../queries";
import { Badge, Button, Card, ConfirmButton, ErrorNote, Field, Icon, Pending, RelativeTime, Spinner, cx } from "../../ui";
import { dayList, dayRange } from "../search/model";
import { useReauthMutation } from "./Reauth";
import { SettingsHeader, SettingsSubhead } from "./Settings";

const methodInfo: Record<GscStatus["method"] & string, string> = {
  oauth: "Google sign-in",
  service_account: "Service account",
  "": "Not connected",
};
const kindInfo = { domain: "Domain property", url_prefix: "URL-prefix property" } as const;

export function SearchConsole() {
  const gsc = useGsc();
  return (
    <>
      <SettingsHeader title="Search Console">
        Byname reads clicks, impressions and position for one property, read-only, once a day. Connect with a Google account that can see the property,
        or with a service account if this instance has no Google OAuth client. The numbers stay when you disconnect.
      </SettingsHeader>
      <GoogleReturn s={gsc.data} />
      {gsc.isPending ? <Pending /> : gsc.isError ? <ErrorNote error={gsc.error} /> : gsc.data.connected ? <Connected s={gsc.data} /> : <Connect s={gsc.data} />}
    </>
  );
}

/** What Google's callback said, from ?connected=google or ?error= on the page it returned to. */
export function GoogleReturn({ s }: { s: GscStatus | undefined }) {
  const [params, setParams] = useSearchParams();
  const failed = params.get("error");
  const dismiss = () => setParams({}, { replace: true });
  return (
    <>
      {params.get("connected") === "google" && (
        <Banner tone="success" onDismiss={dismiss}>
          Connected with Google.{s && !s.property ? " Pick the property to sync below." : ""}
        </Banner>
      )}
      {failed && (
        <Banner tone="danger" onDismiss={dismiss}>
          Connecting with Google failed: {failed}
        </Banner>
      )}
    </>
  );
}

function Banner({ tone, children, onDismiss }: { tone: "success" | "danger"; children: ReactNode; onDismiss: () => void }) {
  return (
    <div role={tone === "danger" ? "alert" : "status"} className={cx("mb-6 flex items-start gap-2 rounded-surface px-3 py-2 text-sm", tone === "success" ? "bg-success-soft text-success" : "bg-danger-soft text-danger")}>
      <Icon name={tone === "success" ? "check" : "alert"} className="mt-0.5 size-4" />
      <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">{children}</span>
      <Button size="sm" variant="ghost" onClick={onDismiss} aria-label="Dismiss" icon className="-my-1">
        <Icon name="x" className="size-3.5" />
      </Button>
    </div>
  );
}

function Connect({ s }: { s: GscStatus }) {
  return (
    <div className="space-y-10">
      {s.data_property && (
        <p className="rounded-surface border border-line bg-surface px-3 py-2 text-sm text-muted">
          Numbers for <span className="font-medium text-fg">{s.data_property}</span> from an earlier connection are kept
          {s.first_day && s.latest_final_day ? `, ${dayRange(s.first_day, s.latest_final_day)}` : ""}. Connect again to keep them current.
        </p>
      )}
      <section>
        <SettingsSubhead>Sign in with Google</SettingsSubhead>
        <p className="mb-3 text-sm text-muted">Byname asks for read-only access to Search Console and nothing else. Use an account that can see the property.</p>
        <GoogleButton available={s.oauth_available} primary />
      </section>
      <section>
        <SettingsSubhead>Or use a service account</SettingsSubhead>
        <ServiceAccountForm />
      </section>
    </div>
  );
}

/** Starts Google's consent page. Google sends the browser back to returnTo's page with ?connected=google or ?error=. */
export function GoogleButton({
  available,
  primary,
  label = "Sign in with Google",
  returnTo = "settings",
}: {
  available: boolean;
  primary?: boolean;
  label?: string;
  returnTo?: "settings" | "onboarding";
}) {
  const google = useReauthMutation({
    mutationFn: () => api.gsc.oauthStart(returnTo),
    onSuccess: ({ url }) => {
      window.location.href = url;
    },
  });
  return (
    <div className="space-y-2">
      <Button variant={primary ? "primary" : "secondary"} onClick={() => google.mutate()} disabled={!available || google.isPending}>
        <Icon name="login" className="size-3.5" />
        {google.isPending ? "Opening Google…" : label}
      </Button>
      {!available && (
        <p className="text-xs text-muted">
          Not available here: this instance has no Google OAuth client. Set <code className="font-mono">GSC_GOOGLE_CLIENT_ID</code> and{" "}
          <code className="font-mono">GSC_GOOGLE_CLIENT_SECRET</code> and restart, or use a service account.
        </p>
      )}
      <ErrorNote error={google.error} />
    </div>
  );
}

export function ServiceAccountForm({ label = "Connect service account" }: { label?: string }) {
  const qc = useQueryClient();
  const [key, setKey] = useState("");
  const account = useReauthMutation({
    mutationFn: (k: string) => api.gsc.serviceAccount(k),
    onSuccess: (st) => {
      setKey("");
      qc.setQueryData(keys.gsc, st);
    },
  });
  const email = clientEmail(key);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (key.trim()) account.mutate(key.trim());
  };
  return (
    <form onSubmit={submit} className="space-y-3">
      <Field
        label="JSON key"
        hint={
          <>
            Create a key for the service account in Google Cloud and paste the whole file. Then, in Search Console, add the service account&apos;s email
            {email ? <> (<span className="font-mono text-fg [overflow-wrap:anywhere]">{email}</span>)</> : <> (its <code className="font-mono">client_email</code>)</>} as a user on the property. Restricted permission is enough. Byname stores the key encrypted and never shows it again.
          </>
        }
      >
        <textarea
          className="block min-h-32 w-full rounded-control border border-line-strong bg-surface px-2.5 py-2 font-mono text-xs text-fg transition-colors placeholder:text-faint hover:border-line-hover focus:border-fg focus:ring-3 focus:ring-halo focus:outline-none"
          placeholder='{"type": "service_account", "client_email": "…", "private_key": "…"}'
          spellCheck={false}
          autoCapitalize="none"
          value={key}
          onChange={(e) => setKey(e.target.value)}
        />
      </Field>
      <ErrorNote error={account.error} />
      <Button type="submit" disabled={!key.trim() || account.isPending}>
        <Icon name="key" className="size-3.5" />
        {label}
      </Button>
    </form>
  );
}

/** The key's client_email, read locally so the hint can name the address to add in Search Console. */
function clientEmail(key: string): string {
  try {
    const v: unknown = JSON.parse(key);
    return v && typeof v === "object" && "client_email" in v && typeof v.client_email === "string" ? v.client_email : "";
  } catch {
    return "";
  }
}

function Connected({ s }: { s: GscStatus }) {
  const qc = useQueryClient();
  const [picking, setPicking] = useState(false);
  useGscProperties();
  const disconnect = useMutation({ mutationFn: api.gsc.disconnect, onSuccess: () => qc.invalidateQueries({ queryKey: keys.search }) });
  const rows: [string, ReactNode][] = [
    ["Method", methodInfo[s.method]],
    ...(s.account ? [["Account", <span className="font-mono text-xs [overflow-wrap:anywhere]">{s.account}</span>] as [string, ReactNode]] : []),
    [
      "Property",
      s.property ? (
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium [overflow-wrap:anywhere]">{s.property}</span>
          {s.property_kind && <Badge>{kindInfo[s.property_kind]}</Badge>}
        </span>
      ) : (
        <span className="text-warn">None picked yet</span>
      ),
    ],
    ["History", s.backfill ? "Every day Google keeps, about 16 months" : "The last 28 days onward"],
    [
      "Last sync",
      s.syncing ? (
        <span className="inline-flex items-center gap-2">
          <Spinner className="size-3.5" /> Syncing now
        </span>
      ) : s.last_sync_at ? (
        <RelativeTime iso={s.last_sync_at} />
      ) : s.property ? (
        <span className="text-muted">Not yet. The first sync starts within a minute.</span>
      ) : (
        <span className="text-muted">Not yet</span>
      ),
    ],
    ...(s.last_sync_error ? [["Last error", <span className="text-danger [overflow-wrap:anywhere]">{s.last_sync_error}</span>] as [string, ReactNode]] : []),
    ["Stored", s.first_day && s.latest_final_day ? `${dayRange(s.first_day, s.latest_final_day)}, final` : <span className="text-muted">Nothing yet</span>],
    ["Partial days", s.partial_days.length ? `${dayList(s.partial_days)}. Google still revises these; the next sync overwrites them.` : <span className="text-muted">None</span>],
  ];

  return (
    <div className="space-y-10">
      <section>
        <Card>
          <dl className="divide-y divide-line">
            {rows.map(([k, v]) => (
              <div key={k} className="grid grid-cols-1 gap-0.5 px-4 py-2.5 text-sm sm:grid-cols-[9rem_minmax(0,1fr)] sm:gap-4">
                <dt className="text-xs font-medium text-muted sm:text-sm sm:font-normal">{k}</dt>
                <dd className="min-w-0">{v}</dd>
              </div>
            ))}
          </dl>
        </Card>
        {s.property && !picking && (
          <Button className="mt-3" onClick={() => setPicking(true)}>
            Change property
          </Button>
        )}
      </section>

      {(!s.property || picking) && (
        <section>
          <SettingsSubhead>{s.property ? "Change property" : "Pick a property"}</SettingsSubhead>
          <PropertyPicker s={s} onDone={() => setPicking(false)} onCancel={s.property ? () => setPicking(false) : undefined} />
        </section>
      )}

      <section>
        <SettingsSubhead>Change the connection</SettingsSubhead>
        <p className="mb-3 text-sm text-muted">
          Connect again to replace the credential. The property stays picked, so use an account that can read it.
        </p>
        <GoogleButton available={s.oauth_available} label={s.method === "oauth" ? "Sign in with Google again" : "Switch to Google sign-in"} />
        <details className="mt-4 text-sm">
          <summary className="cursor-pointer font-medium text-muted hover:text-fg">{s.method === "service_account" ? "Replace the service account key" : "Switch to a service account"}</summary>
          <div className="mt-3">
            <ServiceAccountForm label={s.method === "service_account" ? "Replace key" : "Switch to this service account"} />
          </div>
        </details>
      </section>

      <section>
        <SettingsSubhead>Disconnect</SettingsSubhead>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-sm text-muted">Deletes the stored credential and stops syncing. The numbers already synced stay on the Search page.</p>
          <ConfirmButton label="Disconnect Search Console" confirmLabel="Disconnect?" onConfirm={() => disconnect.mutate()} disabled={disconnect.isPending} className="self-start sm:self-auto">
            <Icon name="x" className="size-3.5" />
            Disconnect
          </ConfirmButton>
        </div>
        <div className="mt-2 empty:hidden">
          <ErrorNote error={disconnect.error} />
        </div>
      </section>
    </div>
  );
}

export function PropertyPicker({ s, onDone, onCancel, submitLabel }: { s: GscStatus; onDone: () => void; onCancel?: () => void; submitLabel?: string }) {
  const qc = useQueryClient();
  const props = useGscProperties();
  const [chosen, setChosen] = useState<string | null>(null);
  const [backfill, setBackfill] = useState(s.property ? s.backfill : true);
  const save = useMutation({
    mutationFn: (p: string) => api.gsc.setProperty(p, backfill),
    onSuccess: (st) => {
      qc.setQueryData(keys.gsc, st);
      onDone();
    },
  });
  if (props.isPending) return <Pending />;
  if (props.isError) return <ErrorNote error={props.error} />;
  if (props.data.length === 0)
    return (
      <p className="rounded-surface border border-dashed border-line-strong px-4 py-6 text-center text-sm text-muted">
        This account can read no Search Console property. Add it as a user on your property in Search Console, then reload this page.
      </p>
    );
  const pick = chosen ?? (props.data.some((p) => p.site_url === s.property) ? s.property : (props.data.find((p) => p.suggested) ?? props.data[0]).site_url);
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate(pick);
      }}
      className="space-y-4"
    >
      <Card>
        <fieldset aria-label="Property" className="divide-y divide-line">
          {props.data.map((p) => (
            <PropertyOption key={p.site_url} p={p} checked={pick === p.site_url} onChange={() => setChosen(p.site_url)} />
          ))}
        </fieldset>
      </Card>
      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" className="mt-0.5" checked={backfill} onChange={(e) => setBackfill(e.target.checked)} />
        <span>
          Backfill history
          <span className="block text-xs text-muted">The first sync pulls every day Google keeps, about 16 months. Off, it starts with the last 28 days.</span>
        </span>
      </label>
      <div className="flex gap-2">
        <Button type="submit" variant="primary" disabled={save.isPending}>
          {submitLabel ?? (s.property ? "Sync this property" : "Start syncing")}
        </Button>
        {onCancel && <Button onClick={onCancel}>Cancel</Button>}
      </div>
      <ErrorNote error={save.error} />
    </form>
  );
}

function PropertyOption({ p, checked, onChange }: { p: GscProperty; checked: boolean; onChange: () => void }) {
  return (
    <label className={cx("flex cursor-pointer items-start gap-3 px-4 py-3 text-sm transition-colors hover:bg-subtle", checked && "bg-subtle")}>
      <input type="radio" name="property" className="mt-0.5" checked={checked} onChange={onChange} />
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium [overflow-wrap:anywhere]">{p.site_url}</span>
          {p.suggested && <Badge tone="info">Matches your domain</Badge>}
        </span>
        <span className="block text-xs text-muted">
          {kindInfo[p.kind]}
          {p.kind === "domain" ? ", every protocol and subdomain" : ", this protocol and host only"} · {permissionInfo[p.permission_level] ?? p.permission_level}
        </span>
      </span>
    </label>
  );
}

const permissionInfo: Record<string, string> = {
  siteOwner: "Owner",
  siteFullUser: "Full user",
  siteRestrictedUser: "Restricted user",
};
