import { useState, type FormEvent, type ReactNode } from "react";
import { Link } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Change, type ChangeKind, type GscStatus, type RepoStatus } from "../../api";
import { keys, useChanges, useChangeSettings, useGsc, useMe, useRepo } from "../../queries";
import { Badge, Button, buttonClass, Card, CardHeader, cx, Dialog, DialogBody, EmptyState, ErrorNote, Field, Icon, inputClass, PageSpinner, RelativeTime, ScrollX, selectClass, useWide } from "../../ui";
import { ViewHeader } from "../results/Header";
import { fullDay } from "../search/model";
import { Note, settingsPath as searchConsolePath } from "../search/Search";
import { ChangeDrawer, ChangeSource, useOpenChange } from "./ChangeDrawer";
import { ControlPagesCard } from "./ControlPages";
import { changeKinds, changeStatus, feed, kindInfo, outcomeInfo, pagePath, siteChangesPath, todayIn, useZone, type Feed, type Zone } from "./model";

export function Changes() {
  const me = useMe();
  const canEdit = !!me.data?.scopes.includes("config:write");
  const admin = me.data?.role === "admin";
  const changes = useChanges();
  const repo = useRepo();
  const gsc = useGsc();
  const zone = useZone();
  const [logging, setLogging] = useState(false);
  return (
    <>
      <ViewHeader
        title="Changes"
        subtitle="Changes to your site's pages, when Google first crawled each one afterwards, and whether the page moved against your control pages."
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
      <div className="relative min-h-0 flex-1 overflow-auto px-3 py-4 md:px-5 md:py-5">
        <div className="mx-auto max-w-6xl space-y-4">
          <Notices gsc={gsc.data} repo={repo.data} admin={admin} zone={zone} />
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

function Notices({ gsc, repo, admin, zone }: { gsc: GscStatus | undefined; repo: RepoStatus | undefined; admin: boolean; zone: Zone }) {
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
      {gsc && !gsc.connected && (
        <Note tone="warn" icon="alert">
          Search Console is not connected, so Byname cannot see Google crawl a changed page or take its reads. Changes are still recorded.
          {fix(searchConsolePath, "Connect Search Console")}
        </Note>
      )}
      {repo?.connected && repo.expiring_soon && repo.token_expires_at && (
        <Note tone="warn" icon="alert">
          The GitHub token expires {zone.date(repo.token_expires_at)}. After that no new merges are picked up.
          {admin ? fix(siteChangesPath, "Replace the token") : " Ask an admin to replace it."}
        </Note>
      )}
      {repo?.connected && repo.last_poll_error && (
        <Note tone="danger" icon="alert">
          The last GitHub check failed: {repo.last_poll_error}
          {fix(siteChangesPath, "Check the connection")}
        </Note>
      )}
      {repo && feed(repo) === "disconnected" && (
        <Note tone="info" icon="note">
          GitHub is disconnected from {repo.repo}, so no new merges are picked up. The changes found so far stay.
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
      ? "Connect your site's GitHub repo in Settings > Site changes, and Byname picks up merged pull requests every hour. Or log a change by hand."
      : "An admin can connect your site's GitHub repo in Settings > Site changes. Changes can also be logged by hand.",
  disconnected: (r) => `GitHub is disconnected from ${r.repo}. Reconnect it in Settings > Site changes, or log a change by hand.`,
  no_rules: (r) => `${r.repo} is connected, but no path rule maps its files to pages yet. Add rules in Settings > Site changes, and the next hourly check finds merges from the last 30 days.`,
  watching: (r) => `No pull request merged into ${r.repo} since it was connected has touched a page the path rules map. Byname checks every hour.`,
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
  const source = feed(repo);
  let body;
  if (error) body = <div className="px-4 pb-4"><ErrorNote error={error} /></div>;
  else if (!changes || !repo || !zone.ready) body = <PageSpinner />;
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
  } else body = wide ? <ChangeTable changes={changes} zone={zone} /> : <ChangeList changes={changes} zone={zone} />;

  return (
    <Card>
      <CardHeader
        title="Site changes"
        description={
          <>
            {source === "watching" && repo ? (
              <>
                Merges into {repo.repo}
                {repo.default_branch && <> ({repo.default_branch})</>}, checked every hour
                {repo.last_poll_at && (
                  <>
                    , last <RelativeTime iso={repo.last_poll_at} />
                  </>
                )}
                .{" "}
              </>
            ) : null}
            {s && `Reads come ${s.interim_days} days (interim) and ${s.verdict_days} days (final) after Google's first crawl. `}
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
    <Badge tone={s.tone} title={s.detail}>
      {s.label}
    </Badge>
  );
}

/** The interim day, with the provisional outcome once it is in. */
function InterimCell({ c }: { c: Change }) {
  if (!c.interim_on) return <span className="text-faint">–</span>;
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      {fullDay(c.interim_on)}
      {c.interim && !c.final && (
        <Badge tone={outcomeInfo[c.interim.outcome].tone} title="Provisional: the final read can differ" className="opacity-80">
          {outcomeInfo[c.interim.outcome].label}
        </Badge>
      )}
    </span>
  );
}

function ChangeTable({ changes, zone }: { changes: Change[]; zone: Zone }) {
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
            <th className={th}>Merged</th>
            <th className={th}>Google&apos;s first crawl after</th>
            <th className={th}>Interim read</th>
            <th className={th}>Final read</th>
            <th className={cx(th, "pr-4")}>Status</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {changes.map((c) => (
            <tr key={c.id} className="cursor-pointer hover:bg-subtle" onClick={(e) => !(e.target as Element).closest("a, button") && open(c.id)}>
              <td className="max-w-72 py-2 pr-3 pl-4 align-top">
                <button type="button" onClick={() => open(c.id)} title={c.url} className="block max-w-full truncate text-left font-medium hover:underline">
                  {pagePath(c.url)}
                </button>
                <span className="text-xs text-faint">{kindInfo[c.kind]}</span>
              </td>
              <td className="max-w-64 py-2 pr-3 align-top">
                <ChangeSource c={c} className="block truncate" />
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
              {fact(c.source === "manual" ? "Went live" : "Merged", zone.date(c.merged_at))}
              {fact("First crawl after", c.first_crawl_at ? zone.at(c.first_crawl_at) : "Not yet")}
              {c.interim_on && fact("Interim read", <InterimCell c={c} />)}
              {c.verdict_on && fact("Final read", fullDay(c.verdict_on))}
            </span>
          </button>
        </li>
      ))}
    </ul>
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
        <p className="text-sm text-muted">For a site without GitHub, or a change made outside it. Byname then watches for Google&apos;s next crawl and takes the reads the same way.</p>
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
            className="block min-h-20 w-full rounded-control border border-line-strong bg-surface px-2.5 py-2 text-sm text-fg transition-colors placeholder:text-faint hover:border-line-hover focus:border-fg focus:ring-3 focus:ring-halo focus:outline-none"
            maxLength={2000}
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
          />
        </Field>
        <ErrorNote error={log.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={onDone}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={!valid || log.isPending}>
            Log change
          </Button>
        </div>
      </form>
    </DialogBody>
  );
}
