import { useQuery } from "@tanstack/react-query";
import { api, type Run, type RunStatus } from "../../api";
import { pct, usd } from "../../lines";
import { keys, useRuns, useSurfaces } from "../../queries";
import { Badge, Card, cx, EmptyState, ErrorNote, Icon, Pending, RelativeTime, type Tone } from "../../ui";
import { runTime } from "../../view";
import { RunNowButton, runStatus, RunStatusBadge, useCanRun } from "../results/RunNow";
import { SettingsHeader, SettingsSubhead } from "./Settings";

function duration(r: Run): string {
  if (!r.started_at) return "–";
  const ms = new Date(r.finished_at ?? Date.now()).getTime() - new Date(r.started_at).getTime();
  if (ms < 1000) return "<1s";
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

const triggers: Record<string, string> = { run_now: "Check now", scheduled: "Scheduled" };
const triggerLabel = (t: string) => triggers[t] ?? t.charAt(0).toUpperCase() + t.slice(1).replaceAll("_", " ");

const jobNames: Record<string, string> = {
  ai_check: "AI check",
  model_refresh: "Model list refresh",
  source_fetch: "Reading linked pages",
  gsc_sync: "Search Console sync",
  social_fetch: "Social search",
  other_brands: "Reading answers for other brands",
  brand_logos: "Looking up logos",
  site_changes: "Site changes from GitHub",
  recrawl_watch: "Waiting for Google to see site changes",
  verdicts: "Site change results",
};

/** A job's status in the same words and tones as a check's; only a job can be skipped. */
const jobStatus = (status: string): { label: string; tone: Tone } =>
  status === "skipped" ? { label: "Skipped", tone: "neutral" } : (runStatus[status as RunStatus] ?? { label: status, tone: "neutral" });

/** The AI app that did worst, which is the one worth a look; all of them when none fell short. */
function AppSummary({ run, title }: { run: Run; title: (endpoint: string) => string }) {
  if (run.endpoints.length === 0) return <span className="text-faint">No answers</span>;
  const worst = run.endpoints.reduce((w, e) => (e.success_rate < w.success_rate ? e : w));
  if (worst.success_rate >= 1) return <span className="text-muted">Every AI app answered every question</span>;
  return (
    <span className="text-muted">
      <span className="text-fg">{title(worst.endpoint)}</span> answered{" "}
      <span className={cx("tabular-nums", worst.success_rate < 0.9 ? "font-semibold text-danger" : "text-fg")}>{pct(worst.success_rate)}</span>
    </span>
  );
}

function MissingCost({ run }: { run: Run }) {
  if (run.spend_missing_calls === 0) return null;
  return (
    <Badge tone="warn">
      <Icon name="alert" className="size-3" />
      {run.spend_missing_calls} without a price
    </Badge>
  );
}

const grid = "xl:grid xl:grid-cols-[10rem_8.5rem_minmax(0,1fr)_5rem_8.5rem_1rem] xl:items-center xl:gap-x-3";

function CheckRow({ run: r, title }: { run: Run; title: (endpoint: string) => string }) {
  return (
    <details className="group">
      <summary className={cx("cursor-pointer list-none px-4 py-2.5 hover:bg-subtle [&::-webkit-details-marker]:hidden", grid)}>
        <div className="flex items-start justify-between gap-3 xl:contents">
          <div>
            <span className="font-medium">Check #{r.id}</span>
            <span className="block text-xs text-muted">
              <RelativeTime iso={runTime(r)} /> · {triggerLabel(r.trigger)} · took <span className="tabular-nums">{duration(r)}</span>
            </span>
          </div>
          <div>
            <RunStatusBadge run={r} />
          </div>
        </div>
        <div className="mt-1 flex items-center justify-between gap-2 text-xs xl:contents">
          <AppSummary run={r} title={title} />
          <Icon name="down" className="size-3.5 text-muted transition-transform group-open:rotate-180 xl:hidden" />
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 xl:contents">
          <span className="tabular-nums xl:text-right">
            {r.succeeded} <span className="text-faint">of {r.calls}</span>
            <span className="text-xs text-muted xl:hidden"> answered</span>
          </span>
          <span className="flex flex-wrap items-center gap-1.5 tabular-nums xl:justify-end">
            {usd(r.spend_usd)}
            <MissingCost run={r} />
          </span>
        </div>
        <Icon name="down" className="hidden size-3.5 text-muted transition-transform group-open:rotate-180 xl:block" />
      </summary>
      <div className="px-4 pb-3">
        {r.error && <p className="mb-2 text-xs text-danger [overflow-wrap:anywhere]">{r.error}</p>}
        {r.spend_missing_calls > 0 && <p className="mb-2 text-xs text-warn">{r.spend_missing_calls} answers reported no price. Their cost is unknown, not $0, so the real spend is higher.</p>}
        <table className="w-full max-w-xl text-xs">
          <caption className="sr-only">Check #{r.id} by AI app</caption>
          <thead className="text-muted">
            <tr>
              <th className="py-1 pr-3 text-left font-medium">AI app</th>
              <th className="py-1 pr-3 text-right font-medium">Answered</th>
              <th className="py-1 text-right font-medium">Cost per answer</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {r.endpoints.map((e) => (
              <tr key={e.endpoint}>
                <td className="py-1 pr-3">
                  {title(e.endpoint)}
                  {e.missing_cost > 0 && <span className="block text-warn">{e.missing_cost} without a price</span>}
                </td>
                <td className={cx("py-1 pr-3 text-right align-top tabular-nums", e.success_rate < 0.9 ? "font-semibold text-danger" : "text-muted")}>
                  {e.succeeded} of {e.calls}
                </td>
                <td className="py-1 text-right align-top text-muted tabular-nums">{e.cost_per_call == null ? "–" : usd(e.cost_per_call)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </details>
  );
}

/** Settings > Check history: every check with what it cost and how each AI app answered. */
export function CheckHistory() {
  const runs = useRuns();
  const canRun = useCanRun();
  const surfaces = useSurfaces();
  // AI apps can share an endpoint (Claude Code and the Claude app), so name it by the ones in use.
  const endpointTitle = new Map<string, string>();
  for (const s of surfaces.data ?? []) if (s.pairs.length > 0) endpointTitle.set(s.endpoint, endpointTitle.has(s.endpoint) ? `${endpointTitle.get(s.endpoint)} / ${s.title}` : s.title);
  const title = (endpoint: string) => endpointTitle.get(endpoint) ?? endpoint;

  let body;
  if (runs.isPending || surfaces.isPending) body = <Pending />;
  else if (runs.isError) body = <ErrorNote error={runs.error} />;
  else if (runs.data.length === 0)
    body = (
      <EmptyState icon="refresh" title="No checks yet" action={canRun ? <RunNowButton /> : undefined}>
        Each check asks every question in every AI app you chose, and records what it cost and how each app answered.
      </EmptyState>
    );
  else
    body = (
      <Card>
        <div className={cx("hidden border-b border-line px-4 py-2 text-xs font-medium text-muted", grid)} aria-hidden="true">
          <span>Check</span>
          <span>Status</span>
          <span>AI apps</span>
          <span className="text-right">Answered</span>
          <span className="text-right">Spent</span>
          <span />
        </div>
        <div className="divide-y divide-line">
          {runs.data.map((r) => (
            <CheckRow key={r.id} run={r} title={title} />
          ))}
        </div>
      </Card>
    );

  return (
    <>
      <SettingsHeader title="Check history">
        Every check with what it cost and how each AI app answered. When one app fails a lot, that is the provider having a bad day, not a drop in your results.
      </SettingsHeader>
      <div className="space-y-8">
        {body}
        <Jobs />
      </div>
    </>
  );
}

function Jobs() {
  const jobs = useQuery({ queryKey: keys.jobs, queryFn: () => api.jobs(20) });
  if (jobs.isPending) return <Pending />;
  if (jobs.isError) return <ErrorNote error={jobs.error} />;
  if (jobs.data.length === 0) return null;
  return (
    <section>
      <SettingsSubhead>Background jobs</SettingsSubhead>
      <p className="-mt-2 mb-3 text-xs text-muted">Besides checks, Byname refreshes model lists, reads the pages AI linked to, syncs Search Console and searches social networks.</p>
      <Card>
        <ul className="divide-y divide-line text-sm">
          {jobs.data.map((j) => {
            const st = jobStatus(j.status);
            return (
              <li key={j.id} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 px-4 py-2 md:grid-cols-[15rem_8.5rem_minmax(0,1fr)_6rem]">
                <span className="font-medium">{jobNames[j.job] ?? j.job}</span>
                <span className="justify-self-end md:justify-self-start">
                  <Badge tone={st.tone}>{st.label}</Badge>
                </span>
                <span className="col-span-2 flex flex-wrap gap-x-2 text-xs text-muted md:col-span-1">
                  <span>{triggerLabel(j.trigger)}</span>
                  {j.run_id && <span className="tabular-nums">Check #{j.run_id}</span>}
                  {j.spend_usd != null && <span className="tabular-nums">{usd(j.spend_usd)}</span>}
                </span>
                <span className="col-span-2 text-xs text-muted md:col-span-1 md:text-right">
                  <RelativeTime iso={j.started_at} />
                </span>
                {j.error && <span className="col-span-2 text-xs text-danger [overflow-wrap:anywhere] md:col-span-4">{j.error}</span>}
              </li>
            );
          })}
        </ul>
      </Card>
    </section>
  );
}
