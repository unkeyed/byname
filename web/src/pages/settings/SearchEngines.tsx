import { useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { api, type SearchMethodInfo, type SearchSite, type SearchStatus } from "../../api";
import { keys, storeEngine, useSearchProviders, useSearchSites } from "../../queries";
import { Badge, Button, Card, ConfirmButton, ErrorNote, Field, Icon, inputClass, Pending, RelativeTime, Spinner, cx } from "../../ui";
import { dayList, dayRange, fullDay, lastDay, siteName } from "../search/model";
import { useReauthMutation } from "./Reauth";
import { SettingsHeader, SettingsSubhead } from "./Settings";

const kindInfo = { domain: "Domain property", url_prefix: "URL-prefix property" } as const;

/** What a pasted key is called in a sentence. */
const keyName = (m: SearchMethodInfo) => (m.method === "service_account" ? "service account key" : m.label);

/** One card per search engine Byname can read, in the server's order. Each connects, picks its site and syncs on its own. */
export function SearchEngines() {
  const providers = useSearchProviders();
  return (
    <>
      <SettingsHeader title="Search engines" />
      <GoogleReturn s={providers.data?.find((s) => s.provider === "google")} />
      {providers.isPending ? (
        <Pending />
      ) : providers.isError ? (
        <ErrorNote error={providers.error} />
      ) : (
        <div className="space-y-12">
          <p className="text-sm text-muted">Byname reads each search engine on its own and never adds them together, because their days and what they count differ.</p>
          {providers.data.map((s) => (
            <Engine key={s.provider} s={s} />
          ))}
        </div>
      )}
    </>
  );
}

function Engine({ s }: { s: SearchStatus }) {
  return (
    <section aria-labelledby={`engine-${s.provider}`} className="space-y-4">
      <div className="flex flex-wrap items-center gap-2 border-b border-line pb-2">
        <h2 id={`engine-${s.provider}`} className="text-base font-semibold">
          {s.title}
        </h2>
        <span className="text-sm text-muted">{s.product}</span>
        <Badge tone={s.connected ? "success" : "neutral"} className="ml-auto">
          {s.connected ? "Connected" : "Not connected"}
        </Badge>
      </div>
      {s.connected ? <Connected s={s} /> : <Connect s={s} />}
    </section>
  );
}

/** What Google's callback said, from ?connected=google or ?error= on the page it returned to. */
export function GoogleReturn({ s }: { s: SearchStatus | undefined }) {
  const [params, setParams] = useSearchParams();
  const failed = params.get("error");
  const dismiss = () => setParams({}, { replace: true });
  return (
    <>
      {params.get("connected") === "google" && (
        <Banner tone="success" onDismiss={dismiss}>
          Connected with Google.{s && !s.site ? " Pick the property to sync below." : ""}
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

/** The ways to connect s that this instance offers, each as its own form, the first one first. */
export function Connect({ s, returnTo = "settings" }: { s: SearchStatus; returnTo?: "settings" | "onboarding" }) {
  const offered = s.methods.filter((m) => m.available);
  return (
    <div className="space-y-8">
      {s.data_site && (
        <p className="rounded-surface border border-line bg-surface px-3 py-2 text-sm text-muted">
          Numbers for <span className="font-medium text-fg">{siteName(s.data_site)}</span> from an earlier connection are kept
          {s.first_day ? `, ${dayRange(s.first_day, lastDay(s))}` : ""}. Connect again to keep them current.
        </p>
      )}
      {offered.map((m, i) => (
        <div key={m.method}>
          <SettingsSubhead>{methodHeading(m, i)}</SettingsSubhead>
          <MethodForm s={s} m={m} returnTo={returnTo} />
        </div>
      ))}
      {s.methods.some((m) => m.method === "oauth" && !m.available) && <NoGoogleSignIn />}
    </div>
  );
}

function methodHeading(m: SearchMethodInfo, i: number): string {
  if (m.method === "service_account") return i === 0 ? "Connect with a service account" : "Or use a service account";
  if (m.method === "api_key") return "Paste your API key";
  return m.label;
}

function MethodForm({ s, m, returnTo }: { s: SearchStatus; m: SearchMethodInfo; returnTo: "settings" | "onboarding" }) {
  if (m.method === "oauth")
    return (
      <>
        <p className="mb-3 text-sm text-muted">Byname asks for read-only access to Search Console and nothing else. Use an account that can see the property.</p>
        <GoogleButton available primary returnTo={returnTo} />
      </>
    );
  if (m.method === "service_account") return <ServiceAccountForm s={s} />;
  return <KeyForm s={s} m={m} />;
}

/** How a server admin adds Google sign-in, shown instead of a button that cannot work. */
function NoGoogleSignIn() {
  return (
    <p className="text-xs text-muted">
      To offer Google sign-in instead, set <code className="font-mono">GSC_GOOGLE_CLIENT_ID</code> and <code className="font-mono">GSC_GOOGLE_CLIENT_SECRET</code> on the
      server and restart Byname.
    </p>
  );
}

/** Starts Google's consent page. Google sends the browser back to returnTo's page with ?connected=google or ?error=. */
function GoogleButton({ available, primary, label = "Sign in with Google", returnTo = "settings" }: { available: boolean; primary?: boolean; label?: string; returnTo?: "settings" | "onboarding" }) {
  const google = useReauthMutation({
    mutationFn: () => api.search.googleOAuthStart(returnTo),
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
      <ErrorNote error={google.error} />
    </div>
  );
}

function ServiceAccountForm({ s, label = "Connect service account" }: { s: SearchStatus; label?: string }) {
  const qc = useQueryClient();
  const [key, setKey] = useState("");
  const account = useReauthMutation({
    mutationFn: (k: string) => api.search.connectKey(s.provider, k),
    onSuccess: (st) => {
      setKey("");
      storeEngine(qc, st);
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
          className="block min-h-32 w-full rounded-control border border-line-strong bg-surface px-2.5 py-2 font-mono text-[16px] md:text-xs text-fg transition-colors placeholder:text-faint hover:border-line-hover focus:border-fg focus:ring-3 focus:ring-halo focus:outline-none"
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

/** A pasted key, such as Bing's API key, with the engine's own steps for getting one. */
function KeyForm({ s, m, label }: { s: SearchStatus; m: SearchMethodInfo; label?: string }) {
  const qc = useQueryClient();
  const [key, setKey] = useState("");
  const connect = useReauthMutation({
    mutationFn: (k: string) => api.search.connectKey(s.provider, k),
    onSuccess: (st) => {
      setKey("");
      storeEngine(qc, st);
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (key.trim()) connect.mutate(key.trim());
  };
  return (
    <form onSubmit={submit} className="space-y-3">
      {m.steps && m.steps.length > 0 && (
        <ol className="list-decimal space-y-1 pl-5 text-sm text-muted">
          {m.steps.map((step) => (
            <li key={step}>{step}</li>
          ))}
        </ol>
      )}
      <Field label={m.label}>
        <input className={inputClass} type="password" autoComplete="off" spellCheck={false} value={key} onChange={(e) => setKey(e.target.value)} placeholder="Paste it here" />
      </Field>
      <ErrorNote error={connect.error} />
      <Button type="submit" variant="primary" disabled={!key.trim() || connect.isPending}>
        <Icon name="key" className="size-3.5" />
        {connect.isPending ? `Checking with ${s.title}…` : (label ?? `Connect ${s.title}`)}
      </Button>
    </form>
  );
}

function Connected({ s }: { s: SearchStatus }) {
  const qc = useQueryClient();
  const [picking, setPicking] = useState(false);
  useSearchSites(s.provider);
  const disconnect = useMutation({ mutationFn: () => api.search.disconnect(s.provider), onSuccess: () => qc.invalidateQueries({ queryKey: keys.search }) });
  const method = s.methods.find((m) => m.method === s.method);
  const rows: [string, ReactNode][] = [
    ["Connected with", method?.label ?? s.method],
    ...(s.account ? [["Account", <span className="font-mono text-xs [overflow-wrap:anywhere]">{s.account}</span>] as [string, ReactNode]] : []),
    [
      "Site",
      s.site ? (
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium [overflow-wrap:anywhere]">{siteName(s.site)}</span>
          {s.site_kind && <Badge>{kindInfo[s.site_kind]}</Badge>}
        </span>
      ) : (
        <span className="text-warn">None picked yet</span>
      ),
    ],
    ["History", s.backfill_months === 0 ? `Everything ${s.title} keeps, read again on every sync` : s.backfill ? `Every day ${s.title} keeps, about ${s.backfill_months} months` : "The last 28 days onward"],
    [
      "Last sync",
      s.syncing ? (
        <span className="inline-flex items-center gap-2">
          <Spinner className="size-3.5" /> Syncing now
        </span>
      ) : s.last_sync_at ? (
        <RelativeTime iso={s.last_sync_at} />
      ) : s.site ? (
        <span className="text-muted">Not yet. The first sync starts within a minute.</span>
      ) : (
        <span className="text-muted">Not yet</span>
      ),
    ],
    ...(s.last_sync_error ? [["Last error", <span className="text-danger [overflow-wrap:anywhere]">{s.last_sync_error}</span>] as [string, ReactNode]] : []),
    ["Stored", s.first_day ? dayRange(s.first_day, lastDay(s)) : <span className="text-muted">Nothing yet</span>],
    ...(s.latest_final_day
      ? [["Totals up to", `${fullDay(s.latest_final_day)}, the ${s.step_days > 1 ? "end of the latest week" : "latest day"} ${s.title} has finished counting`] as [string, ReactNode]]
      : []),
    ["Still updating", s.partial_days.length ? `${dayList(s.partial_days)}. ${s.title} still revises these days, and the next sync replaces them.` : <span className="text-muted">None</span>],
  ];
  const keyMethods = s.methods.filter((m) => m.method !== "oauth");
  const signIn = s.methods.find((m) => m.method === "oauth" && m.available);

  return (
    <div className="space-y-8">
      <div>
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
        {s.site && !picking && (
          <Button className="mt-3" onClick={() => setPicking(true)}>
            Change site
          </Button>
        )}
      </div>

      {(!s.site || picking) && (
        <div>
          <SettingsSubhead>{s.site ? "Change site" : "Pick a site"}</SettingsSubhead>
          <SitePicker s={s} onDone={() => setPicking(false)} onCancel={s.site ? () => setPicking(false) : undefined} />
        </div>
      )}

      <div>
        <SettingsSubhead>Change the connection</SettingsSubhead>
        <p className="mb-3 text-sm text-muted">Connect again to change how Byname signs in to {s.title}. The site stays picked, so use an account that can read it.</p>
        {signIn && <GoogleButton available label={s.method === "oauth" ? "Sign in with Google again" : "Switch to Google sign-in"} />}
        {keyMethods.map((m) => (
          <details key={m.method} className={cx("text-sm", signIn && "mt-4")}>
            <summary className="cursor-pointer py-3.5 font-medium text-muted hover:text-fg md:py-0">{s.method === m.method ? `Replace the ${keyName(m)}` : m.method === "service_account" ? "Switch to a service account" : `Switch to ${m.label}`}</summary>
            <div className="mt-3">
              {m.method === "service_account" ? (
                <ServiceAccountForm s={s} label={s.method === m.method ? "Replace key" : "Switch to this service account"} />
              ) : (
                <KeyForm s={s} m={m} label={s.method === m.method ? "Replace key" : undefined} />
              )}
            </div>
          </details>
        ))}
      </div>

      <div>
        <SettingsSubhead>Disconnect</SettingsSubhead>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-sm text-muted">Deletes the stored sign-in and stops syncing. The numbers already synced stay under Search, Traffic.</p>
          <ConfirmButton label={`Disconnect ${s.product}`} confirmLabel="Disconnect?" onConfirm={() => disconnect.mutate()} disabled={disconnect.isPending} className="self-start sm:self-auto">
            <Icon name="x" className="size-3.5" />
            Disconnect
          </ConfirmButton>
        </div>
        <div className="mt-2 empty:hidden">
          <ErrorNote error={disconnect.error} />
        </div>
      </div>
    </div>
  );
}

/** Picks the site s syncs, the one matching the brand's domain preselected. */
export function SitePicker({ s, onDone, onCancel, submitLabel }: { s: SearchStatus; onDone: () => void; onCancel?: () => void; submitLabel?: string }) {
  const qc = useQueryClient();
  const sites = useSearchSites(s.provider);
  const [chosen, setChosen] = useState<string | null>(null);
  const [backfill, setBackfill] = useState(s.site ? s.backfill : true);
  const save = useMutation({
    mutationFn: (site: string) => api.search.setSite(s.provider, site, backfill),
    onSuccess: (st) => {
      storeEngine(qc, st);
      onDone();
    },
  });
  if (sites.isPending) return <Pending />;
  if (sites.isError) return <ErrorNote error={sites.error} />;
  if (sites.data.length === 0)
    return (
      <p className="rounded-surface border border-dashed border-line-strong px-4 py-6 text-center text-sm text-muted">
        {s.product} lists no site this connection can read. Add or verify your site in {s.product}, then reload this page.
      </p>
    );
  const pick = chosen ?? (sites.data.some((x) => x.url === s.site) ? s.site : (sites.data.find((x) => x.suggested) ?? sites.data[0]).url);
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate(pick);
      }}
      className="space-y-4"
    >
      <Card>
        <fieldset aria-label="Site" className="divide-y divide-line">
          {sites.data.map((x) => (
            <SiteOption key={x.url} site={x} checked={pick === x.url} onChange={() => setChosen(x.url)} />
          ))}
        </fieldset>
      </Card>
      {s.backfill_months > 0 && (
        <label className="flex items-start gap-2 text-sm">
          <input type="checkbox" className="mt-0.5" checked={backfill} onChange={(e) => setBackfill(e.target.checked)} />
          <span>
            Backfill history
            <span className="block text-xs text-muted">
              The first sync pulls every day {s.title} keeps, about {s.backfill_months} months. Off, it starts with the last 28 days.
            </span>
          </span>
        </label>
      )}
      <div className="flex gap-2">
        <Button type="submit" variant="primary" disabled={save.isPending}>
          {submitLabel ?? (s.site ? "Sync this site" : "Start syncing")}
        </Button>
        {onCancel && <Button onClick={onCancel}>Cancel</Button>}
      </div>
      <ErrorNote error={save.error} />
    </form>
  );
}

function SiteOption({ site, checked, onChange }: { site: SearchSite; checked: boolean; onChange: () => void }) {
  const detail = [site.kind && `${kindInfo[site.kind]}, ${site.kind === "domain" ? "every protocol and subdomain" : "this protocol and host only"}`, site.permission && (permissionInfo[site.permission] ?? site.permission)].filter(Boolean).join(" · ");
  return (
    <label className={cx("flex cursor-pointer items-start gap-3 px-4 py-3 text-sm transition-colors hover:bg-subtle", checked && "bg-subtle")}>
      <input type="radio" name="site" className="mt-0.5" checked={checked} onChange={onChange} />
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium [overflow-wrap:anywhere]">{site.url}</span>
          {site.suggested && <Badge tone="info">Matches your domain</Badge>}
        </span>
        {detail && <span className="block text-xs text-muted">{detail}</span>}
      </span>
    </label>
  );
}

const permissionInfo: Record<string, string> = {
  siteOwner: "Owner",
  siteFullUser: "Full user",
  siteRestrictedUser: "Restricted user",
};
