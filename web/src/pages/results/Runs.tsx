import { useQuery } from "@tanstack/react-query";
import { api, type Run, type RunStatus } from "../../api";
import { pct, usd } from "../../lines";
import { keys, useRuns, useSurfaces } from "../../queries";
import { Badge, Card, cx, EmptyState, ErrorNote, Icon, Pending, RelativeTime, type Tone } from "../../ui";
import { runTime, useFilter } from "../../view";
import { ViewHeader } from "./Header";
import { RunNowButton, runStatus, RunStatusBadge, useCanRun } from "./RunNow";

function duration(r: Run): string {
  if (!r.started_at) return "–";
  const ms = new Date(r.finished_at ?? Date.now()).getTime() - new Date(r.started_at).getTime();
  if (ms < 1000) return "<1s";
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

const triggers: Record<string, string> = { run_now: "Run now", scheduled: "Scheduled" };
const triggerLabel = (t: string) => triggers[t] ?? t.charAt(0).toUpperCase() + t.slice(1).replaceAll("_", " ");

const jobNames: Record<string, string> = { ai_check: "AI check", model_refresh: "Model lists", source_fetch: "Cited pages", gsc_sync: "Search Console sync", social_fetch: "Social search" };

/** A job's status in the same words and tones as a run's; only a job can be skipped. */
const jobStatus = (status: string): { label: string; tone: Tone } =>
  status === "skipped" ? { label: "Skipped", tone: "neutral" } : (runStatus[status as RunStatus] ?? { label: status, tone: "neutral" });

/** The endpoint that did worst, which is the one worth a look; all of them when none fell short. */
function EndpointSummary({ run, title }: { run: Run; title: (endpoint: string) => string }) {
  if (run.endpoints.length === 0) return <span className="text-faint">No calls</span>;
  const worst = run.endpoints.reduce((w, e) => (e.success_rate < w.success_rate ? e : w));
  if (worst.success_rate >= 1) return <span className="text-muted">All {run.endpoints.length} answered every call</span>;
  return (
    <span className="text-muted">
      Lowest <span className="text-fg">{title(worst.endpoint)}</span>{" "}
      <span className={cx("tabular-nums", worst.success_rate < 0.9 ? "font-semibold text-danger" : "text-fg")} title={`${worst.succeeded} of ${worst.calls} calls answered`}>
        {pct(worst.success_rate)}
      </span>
    </span>
  );
}

function MissingCost({ run }: { run: Run }) {
  if (run.spend_missing_calls === 0) return null;
  return (
    <Badge tone="warn" title="These calls reported no cost. Their cost is unknown, not $0, so the real spend is higher.">
      <Icon name="alert" className="size-3" />
      {run.spend_missing_calls} without a cost
    </Badge>
  );
}

const runGrid = "lg:grid lg:grid-cols-[7.5rem_7rem_5rem_3rem_6.5rem_11.5rem_minmax(0,1fr)_1rem] lg:items-center lg:gap-x-3";

function RunRow({ run: r, title }: { run: Run; title: (endpoint: string) => string }) {
  return (
    <details className="group">
      <summary className={cx("cursor-pointer list-none px-4 py-2.5 hover:bg-subtle [&::-webkit-details-marker]:hidden", runGrid)}>
        <div className="flex items-start justify-between gap-3 lg:contents">
          <div>
            <span className="font-medium">#{r.id}</span>
            <span className="text-xs text-muted lg:block">
              <span className="lg:hidden"> · </span>
              <RelativeTime iso={runTime(r)} />
            </span>
          </div>
          <div className="lg:order-none">
            <RunStatusBadge run={r} />
          </div>
        </div>
        <div className="mt-1 text-xs text-muted lg:contents">
          <span>{triggerLabel(r.trigger)}</span>
          <span className="lg:hidden"> · took </span>
          <span className="tabular-nums">{duration(r)}</span>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 lg:contents">
          <span className="tabular-nums lg:text-right">
            {r.succeeded} <span className="text-faint">of {r.calls}</span>
            <span className="text-xs text-muted lg:hidden"> answered</span>
          </span>
          <span className="flex flex-wrap items-center gap-1.5 tabular-nums lg:justify-end">
            {usd(r.spend_usd)}
            <MissingCost run={r} />
          </span>
        </div>
        <div className="mt-1 flex items-center justify-between gap-2 text-xs lg:contents">
          <EndpointSummary run={r} title={title} />
          <span className="flex items-center gap-1 text-muted">
            <span className="lg:sr-only">Endpoints</span>
            <Icon name="down" className="size-3.5 transition-transform group-open:rotate-180" />
          </span>
        </div>
      </summary>
      <div className="px-4 pb-3 lg:pl-[calc(1rem+4.5rem+0.75rem)]">
        {r.error && <p className="mb-2 text-xs text-danger [overflow-wrap:anywhere]">{r.error}</p>}
        <table className="w-full max-w-xl text-xs">
          <caption className="sr-only">Check #{r.id} by endpoint</caption>
          <thead className="text-muted">
            <tr>
              <th className="py-1 pr-3 text-left font-medium">Endpoint</th>
              <th className="py-1 pr-3 text-right font-medium">Answered</th>
              <th className="py-1 text-right font-medium">Cost per call</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {r.endpoints.map((e) => (
              <tr key={e.endpoint}>
                <td className="py-1 pr-3" title={e.endpoint}>
                  {title(e.endpoint)}
                  {e.missing_cost > 0 && <span className="block text-warn">{e.missing_cost} without a cost</span>}
                </td>
                <td className={cx("py-1 pr-3 text-right align-top tabular-nums", e.success_rate < 0.9 ? "font-semibold text-danger" : "text-muted")} title={`${e.succeeded} of ${e.calls} calls answered`}>
                  {pct(e.success_rate)}
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

export function Runs() {
  const runs = useRuns();
  const canRun = useCanRun();
  const { filter } = useFilter();
  const surfaces = useSurfaces();
  // Surfaces can share an endpoint (Claude Code and the Claude app), so name it by the ones in use.
  const endpointTitle = new Map<string, string>();
  for (const s of surfaces.data ?? []) if (s.pairs.length > 0) endpointTitle.set(s.endpoint, endpointTitle.has(s.endpoint) ? `${endpointTitle.get(s.endpoint)} / ${s.title}` : s.title);
  const title = (endpoint: string) => endpointTitle.get(endpoint) ?? endpoint;
  const shown = (runs.data ?? []).filter((r) => !filter.since || runTime(r) >= filter.since);

  let body;
  if (runs.isPending) body = <Pending />;
  else if (runs.isError) body = <ErrorNote error={runs.error} />;
  else if (runs.data.length === 0)
    body = (
      <EmptyState icon="refresh" title="No checks yet" action={canRun ? <RunNowButton /> : undefined}>
        Each check asks every question on every surface and model, and records what it cost and how each endpoint did.
      </EmptyState>
    );
  else
    body = (
      <Card>
        <div className={cx("hidden border-b border-line px-4 py-2 text-xs font-medium text-muted", runGrid)} aria-hidden="true">
          <span>Check</span>
          <span>Status</span>
          <span>Trigger</span>
          <span>Took</span>
          <span className="text-right">Calls answered</span>
          <span className="text-right">Spend</span>
          <span>Endpoints</span>
          <span />
        </div>
        <div className="divide-y divide-line">
          {shown.map((r) => (
            <RunRow key={r.id} run={r} title={title} />
          ))}
        </div>
        {shown.length === 0 && <p className="px-4 py-8 text-center text-sm text-muted">No check in this range.</p>}
      </Card>
    );

  return (
    <>
      <ViewHeader
        title="Runs"
        subtitle="Every check with its spend, cost per call and success rate per endpoint, so a flaky provider shows up as a provider problem."
        scope={false}
        action={canRun && runs.data?.length ? <RunNowButton /> : undefined}
      />
      <div className="relative min-h-0 flex-1 overflow-auto px-3 py-4 lg:px-5 lg:py-5">
        <div className="mx-auto max-w-6xl space-y-6">
          {body}
          <Jobs />
        </div>
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
      <h2 className="mb-2 text-sm font-semibold">Recent jobs</h2>
      <Card>
        <ul className="divide-y divide-line text-sm">
          {jobs.data.map((j) => {
            const st = jobStatus(j.status);
            return (
              <li key={j.id} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 px-4 py-2 md:grid-cols-[7rem_10rem_6rem_6rem_5rem_minmax(0,1fr)]">
                <span className="font-medium">{jobNames[j.job] ?? j.job}</span>
                <span className="justify-self-end md:justify-self-start">
                  <Badge tone={st.tone}>{st.label}</Badge>
                </span>
                <span className="col-span-2 flex flex-wrap gap-x-2 text-xs text-muted md:contents">
                  <span>{triggerLabel(j.trigger)}</span>
                  <span className={cx("tabular-nums", !j.run_id && "hidden text-faint md:inline")}>{j.run_id ? `Check #${j.run_id}` : "–"}</span>
                  <span className={cx("tabular-nums", j.spend_usd == null && "hidden text-faint md:inline")}>{j.spend_usd != null ? usd(j.spend_usd) : "–"}</span>
                  <span className="md:text-right">
                    <RelativeTime iso={j.started_at} />
                  </span>
                </span>
                {j.error && <span className="col-span-2 text-xs text-danger [overflow-wrap:anywhere] md:col-span-6">{j.error}</span>}
              </li>
            );
          })}
        </ul>
      </Card>
    </section>
  );
}
