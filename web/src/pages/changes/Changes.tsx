import { useState, type FormEvent, type ReactNode } from "react";
import { Link } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Change, type ChangeKind, type RepoStatus, type SearchStatus } from "../../api";
import { keys, useChanges, useChangeSettings, useMe, useRepo, useSearchProviders } from "../../queries";
import { Badge, Button, buttonClass, Card, CardHeader, cx, Dialog, DialogBody, EmptyState, ErrorNote, Field, Icon, inputClass, Pending, RelativeTime, RowButton, scrollArea, ScrollX, selectClass, useLoading, useWide, type Tone } from "../../ui";
import { ViewHeader } from "../results/Header";
import { fullDay } from "../search/model";
import { Note, settingsPath as searchEnginesPath } from "../search/Search";
import { ChangeDrawer, ChangeSource, useOpenChange } from "./ChangeDrawer";
import { ControlPagesCard } from "./ControlPages";
import { changeKinds, changeStatus, feed, kindInfo, outcomeInfo, pagePath, siteChangesPath, stageInfo, todayIn, useZone, type Feed, type Zone } from "./model";

export function Changes() {
  const me = useMe();
  const canEdit = !!me.data?.scopes.includes("config:write");
  const admin = me.data?.role === "admin";
  const changes = useChanges();
  const repo = useRepo();
  const providers = useSearchProviders();
  const google = providers.data?.find((s) => s.provider === "google");
  const zone = useZone();
  const [logging, setLogging] = useState(false);
  useLoading(providers.isPending);
  return (
    <>
      <ViewHeader
        title="Site changes"
        range={false}
        scope={false}
        action={
          canEdit && (
            <Button size="sm" variant="primary" onClick={() => setLogging(true)}>
              <Icon name="plus" className="size-3.5" />
              Log a change
            </Button>
          )
        }
      />
      <div className={cx("relative min-h-0 flex-1 px-3 py-4 md:px-5 md:py-5", scrollArea)}>
        <div className="mx-auto max-w-6xl space-y-4">
          <Notices google={google} repo={repo.data} admin={admin} zone={zone} />
          <ChangesCard changes={changes.data} error={changes.error ?? repo.error} repo={repo.data} admin={admin} canEdit={canEdit} zone={zone} onLog={() => setLogging(true)} />
          <ControlPagesCard canEdit={canEdit} />
        </div>
      </div>
      <ChangeDrawer changes={changes.data} canEdit={canEdit} />
      <Dialog open={logging} onClose={() => setLogging(false)} title="Log a change">
        <LogForm zone={zone} onDone={() => setLogging(false)} />
      </Dialog>
    </>
  );
}

function Notices({ google, repo, admin, zone }: { google: SearchStatus | undefined; repo: RepoStatus | undefined; admin: boolean; zone: Zone }) {
  const fix = (to: string, label: string) =>
    admin && (
      <>
        {" "}
        <Link to={to} className="font-medium underline">
          {label}
        </Link>
      </>
    );
  return (
    <>
      {google && !google.connected && (
        <Note tone="warn" icon="alert">
          Google Search Console is not connected, so Byname can&apos;t see when Google saw a changed page or measure its results. Changes are still recorded.
          {fix(searchEnginesPath, "Connect Search Console")}
        </Note>
      )}
      {repo?.connected && repo.expiring_soon && repo.token_expires_at && (
        <Note tone="warn" icon="alert">
          The GitHub token expires {zone.date(repo.token_expires_at)}. After that Byname stops finding new changes.
          {admin ? fix(siteChangesPath, "Replace the token") : " Ask an admin to replace it."}
        </Note>
      )}
      {repo?.connected && repo.last_poll_error && (
        <Note tone="danger" icon="alert">
          Byname couldn&apos;t read GitHub last time: {repo.last_poll_error}
          {fix(siteChangesPath, "Check the connection")}
        </Note>
      )}
      {repo && feed(repo) === "disconnected" && (
        <Note tone="info" icon="note">
          GitHub is disconnected from {repo.repo}, so Byname stops finding new changes. The changes found so far stay.
          {fix(siteChangesPath, "Reconnect")}
        </Note>
      )}
    </>
  );
}

/** What an empty list says, for each place changes can come from. */
const emptyBody: Record<Feed, (r: RepoStatus, admin: boolean) => string> = {
  none: (_, admin) =>
    admin
      ? "Connect your site's GitHub repo in Settings > Site changes, and Byname finds merged pull requests every hour. Or log a change by hand."
      : "An admin can connect your site's GitHub repo in Settings > Site changes. Changes can also be logged by hand.",
  disconnected: (r) => `GitHub is disconnected from ${r.repo}. Reconnect it in Settings > Site changes, or log a change by hand.`,
  no_rules: (r) => `${r.repo} is connected, but no path rule says which files are which pages yet. Add rules in Settings > Site changes, and within the hour Byname finds merges from the last 30 days.`,
  watching: (r) => `No pull request merged into ${r.repo} since it was connected has touched a page the path rules cover. Byname looks every hour.`,
};

function ChangesCard({
  changes,
  error,
  repo,
  admin,
  canEdit,
  zone,
  onLog,
}: {
  changes: Change[] | undefined;
  error: unknown;
  repo: RepoStatus | undefined;
  admin: boolean;
  canEdit: boolean;
  zone: Zone;
  onLog: () => void;
}) {
  const wide = useWide();
  const settings = useChangeSettings();
  const s = settings.data;
  useLoading(settings.isPending);
  const source = feed(repo);
  let body;
  if (error) body = <div className="px-4 pb-4"><ErrorNote error={error} /></div>;
  else if (!changes || !repo || !zone.ready) body = <Pending />;
  else if (changes.length === 0) {
    body = (
      <div className="px-4 pb-4">
        <EmptyState
          icon="merge"
          title="No changes yet"
          action={
            (admin || canEdit) && (
              <div className="flex flex-wrap justify-center gap-2">
                {admin && source !== "watching" && (
                  <Link to={siteChangesPath} className={buttonClass({ variant: "primary" })}>
                    <Icon name="github" className="size-3.5" />
                    {source === "no_rules" ? "Add path rules" : "Connect GitHub"}
                  </Link>
                )}
                {canEdit && (
                  <Button onClick={onLog}>
                    <Icon name="plus" className="size-3.5" />
                    Log a change
                  </Button>
                )}
              </div>
            )
          }
        >
          {emptyBody[source](repo, admin)}
        </EmptyState>
      </div>
    );
  } else
    body = (
      <>
        {wide ? <ChangeTable changes={changes} zone={zone} interimDays={s?.interim_days} verdictDays={s?.verdict_days} /> : <ChangeList changes={changes} zone={zone} />}
        <ResultKey />
      </>
    );

  return (
    <Card>
      <CardHeader
        title="Your changes"
        description={
          <>
            {source === "watching" && repo ? (
              <>
                Merges into {repo.repo}
                {repo.default_branch && <> ({repo.default_branch})</>}, looked at every hour
                {repo.last_poll_at && (
                  <>
                    , last <RelativeTime iso={repo.last_poll_at} />
                  </>
                )}
                .{" "}
              </>
            ) : null}
            {s && `The first result comes ${s.interim_days} days after Google saw the change, the final result ${s.verdict_days} days after. `}
            Results are experimental: check a surprising one against Traffic before acting on it.{" "}
            Times are in {zone.zone}.
          </>
        }
      />
      {body}
    </Card>
  );
}

function StatusBadge({ c }: { c: Change }) {
  const s = changeStatus(c);
  return (
    <Badge tone={s.tone}>{s.label}</Badge>
  );
}

/** The first result's day, with its early outcome once it is in. */
function InterimCell({ c }: { c: Change }) {
  if (!c.interim_on) return <span className="text-faint">–</span>;
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      {fullDay(c.interim_on)}
      {c.interim && !c.final && (
        <Badge tone={outcomeInfo[c.interim.outcome].tone} className="opacity-80">
          {outcomeInfo[c.interim.outcome].label}
        </Badge>
      )}
    </span>
  );
}

const after = (days: number | undefined) => (days ? ` (${days} days)` : "");

function ChangeTable({ changes, zone, interimDays, verdictDays }: { changes: Change[]; zone: Zone; interimDays?: number; verdictDays?: number }) {
  const open = useOpenChange();
  const th = "py-1.5 pr-3 font-medium";
  const td = "py-2 pr-3 align-top whitespace-nowrap";
  return (
    <ScrollX label="site changes">
      <table className="w-full min-w-[60rem] text-left text-sm">
        <thead className="text-xs text-muted">
          <tr className="border-y border-line">
            <th className={cx(th, "pl-4")}>Page</th>
            <th className={th}>Change</th>
            <th className={th}>Went live</th>
            <th className={th}>Google saw it</th>
            <th className={th}>First result{after(interimDays)}</th>
            <th className={th}>Final result{after(verdictDays)}</th>
            <th className={cx(th, "pr-4")}>Result</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {changes.map((c) => (
            <tr key={c.id} className="group relative hover:bg-subtle">
              <td className="max-w-72 py-2 pr-3 pl-4 align-top">
                <RowButton label={pagePath(c.url)} onClick={() => open(c.id)} />
                <span className="block max-w-full truncate font-medium group-hover:underline">{pagePath(c.url)}</span>
                <span className="text-xs text-faint">{kindInfo[c.kind]}</span>
              </td>
              <td className="max-w-64 py-2 pr-3 align-top">
                <ChangeSource c={c} className="relative z-[3] -my-1 block truncate py-1" />
              </td>
              <td className={td}>{zone.date(c.merged_at)}</td>
              <td className={td}>{c.first_crawl_at ? zone.at(c.first_crawl_at) : <span className="text-muted">Not yet</span>}</td>
              <td className={td}>
                <InterimCell c={c} />
              </td>
              <td className={td}>{c.verdict_on ? fullDay(c.verdict_on) : <span className="text-faint">–</span>}</td>
              <td className={cx(td, "pr-4")}>
                <StatusBadge c={c} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </ScrollX>
  );
}

function ChangeList({ changes, zone }: { changes: Change[]; zone: Zone }) {
  const open = useOpenChange();
  const fact = (label: string, value: ReactNode) => (
    <span className="contents">
      <span className="text-muted">{label}</span>
      <span className="text-right">{value}</span>
    </span>
  );
  return (
    <ul aria-label="Site changes" className="divide-y divide-line border-t border-line">
      {changes.map((c) => (
        <li key={c.id}>
          <button type="button" onClick={() => open(c.id)} className="block w-full px-4 py-3 text-left hover:bg-subtle">
            <span className="flex items-start gap-2">
              <span className="min-w-0 flex-1">
                <span className="block font-medium [overflow-wrap:anywhere]">{pagePath(c.url)}</span>
                <span className="block text-xs text-faint">
                  {kindInfo[c.kind]} · {c.source === "manual" ? "Logged by hand" : `#${c.pr_number} ${c.pr_title}`}
                </span>
              </span>
              <StatusBadge c={c} />
            </span>
            <span className="mt-2 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
              {fact("Went live", zone.date(c.merged_at))}
              {fact("Google saw it", c.first_crawl_at ? zone.at(c.first_crawl_at) : "Not yet")}
              {c.interim_on && fact("First result", <InterimCell c={c} />)}
              {c.verdict_on && fact("Final result", fullDay(c.verdict_on))}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

/** What each result word means, since a badge in a table can't carry its own explanation. */
function ResultKey() {
  const rows: { label: string; tone: Tone; detail: string }[] = [
    ...(["improved", "worsened", "no_change", "insufficient_data"] as const).map((o) => outcomeInfo[o]),
    { ...stageInfo.awaiting_crawl, detail: "Google hasn't looked at the page since the change." },
    { ...stageInfo.awaiting_interim, detail: "Google saw the change. A faded word next to the first result's date is early: the final result can differ." },
    { ...stageInfo.awaiting_verdict, detail: "The first result is in. The final result is still to come." },
  ];
  return (
    <dl aria-label="What the results mean" className="grid grid-cols-1 gap-x-4 gap-y-1.5 border-t border-line px-4 py-3 text-xs lg:grid-cols-2">
      {rows.map((r) => (
        <div key={r.label} className="flex items-start gap-2">
          <dt className="shrink-0">
            <Badge tone={r.tone}>{r.label}</Badge>
          </dt>
          <dd className="pt-0.5 text-muted">{r.detail}</dd>
        </div>
      ))}
    </dl>
  );
}

function LogForm({ zone, onDone }: { zone: Zone; onDone: () => void }) {
  const qc = useQueryClient();
  const open = useOpenChange();
  const today = todayIn(zone.zone);
  const [url, setUrl] = useState("");
  const [kind, setKind] = useState<ChangeKind>("edit");
  const [day, setDay] = useState(today);
  const [notes, setNotes] = useState("");
  const log = useMutation({
    mutationFn: () => api.changes.log({ url: url.trim(), kind, merged_at: day, notes: notes.trim() || undefined }),
    onSuccess: async (c) => {
      await qc.invalidateQueries({ queryKey: keys.changes });
      onDone();
      open(c.id);
    },
  });
  const valid = /^https?:\/\/\S+$/.test(url.trim()) && /^\d{4}-\d{2}-\d{2}$/.test(day) && day <= today;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (valid) log.mutate();
  };
  return (
    <DialogBody>
      <form onSubmit={submit} className="space-y-4">
        <p className="text-sm text-muted">For a site without GitHub, or a change made outside it. Byname then waits for Google to see the page and measures the results the same way.</p>
        <Field label="Page URL" hint="The full URL, as Search Console reports it.">
          <input
            className={inputClass}
            type="url"
            inputMode="url"
            autoCapitalize="none"
            spellCheck={false}
            required
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://www.example.com/pricing"
            data-autofocus
          />
        </Field>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="What changed">
            <select className={cx(selectClass, "w-full")} value={kind} onChange={(e) => setKind(e.target.value as ChangeKind)}>
              {changeKinds.map((k) => (
                <option key={k} value={k}>
                  {kindInfo[k]}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Went live on" hint={`From midnight in ${zone.zone}.`}>
            <input className={inputClass} type="date" required max={today} value={day} onChange={(e) => setDay(e.target.value)} />
          </Field>
        </div>
        <Field label="Notes" hint="Optional, such as what you changed.">
          <textarea
            className="block min-h-20 w-full rounded-control border border-line-strong bg-surface px-2.5 py-2 text-[16px] md:text-sm text-fg transition-colors placeholder:text-faint hover:border-line-hover focus:border-fg focus:ring-3 focus:ring-halo focus:outline-none"
            maxLength={2000}
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
          />
        </Field>
        <div className="flex justify-end gap-2">
          <Button onClick={onDone}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={!valid || log.isPending}>
            Log change
          </Button>
        </div>
        <ErrorNote error={log.error} />
      </form>
    </DialogBody>
  );
}
