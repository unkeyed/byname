import { useQuery } from "@tanstack/react-query";
import { api, type Run } from "../../api";
import { pct, usd } from "../../lines";
import { keys, useRuns, useSurfaces } from "../../queries";
import { Badge, Card, cx, EmptyState, ErrorNote, Icon, PageSpinner, RelativeTime } from "../../ui";
import { runTime, useFilter } from "../../view";
import { ViewHeader } from "./Header";
import { RunNowButton, RunStatusBadge, useCanRun } from "./RunNow";

function duration(r: Run): string {
  if (!r.started_at) return "–";
  const ms = new Date(r.finished_at ?? Date.now()).getTime() - new Date(r.started_at).getTime();
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

export function Runs() {
  const runs = useRuns();
  const canRun = useCanRun();
  const { filter } = useFilter();
  const surfaces = useSurfaces();
  // Surfaces can share an endpoint (Claude Code and the Claude app), so name it by the ones in use.
  const endpointTitle = new Map<string, string>();
  for (const s of surfaces.data ?? []) if (s.pairs.length > 0) endpointTitle.set(s.endpoint, endpointTitle.has(s.endpoint) ? `${endpointTitle.get(s.endpoint)} / ${s.title}` : s.title);
  const shown = (runs.data ?? []).filter((r) => !filter.since || runTime(r) >= filter.since);

  let body;
  if (runs.isPending) body = <PageSpinner />;
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
        <div className="overflow-x-auto">
          <table className="w-full min-w-max text-sm">
            <thead className="border-b border-line text-xs text-muted">
              <tr>
                <th className="px-4 py-2 text-left font-medium">Check</th>
                <th className="px-3 py-2 text-left font-medium">Status</th>
                <th className="px-3 py-2 text-left font-medium">Trigger</th>
                <th className="px-3 py-2 text-left font-medium">Took</th>
                <th className="px-3 py-2 text-right font-medium">Calls answered</th>
                <th className="px-3 py-2 text-right font-medium">Spend</th>
                <th className="px-3 py-2 text-left font-medium">Success rate and cost per call, by endpoint</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {shown.map((r) => (
                <tr key={r.id} className="align-top">
                  <td className="px-4 py-2.5">
                    <div className="font-medium">#{r.id}</div>
                    <div className="text-xs text-muted">
                      <RelativeTime iso={runTime(r)} />
                    </div>
                  </td>
                  <td className="px-3 py-2.5">
                    <RunStatusBadge run={r} />
                    {r.error && <div className="mt-1 max-w-56 text-xs text-danger [overflow-wrap:anywhere]">{r.error}</div>}
                  </td>
                  <td className="px-3 py-2.5 text-xs text-muted">{r.trigger.replace("_", " ")}</td>
                  <td className="px-3 py-2.5 text-xs text-muted tabular-nums">{duration(r)}</td>
                  <td className="px-3 py-2.5 text-right tabular-nums">
                    {r.succeeded} <span className="text-faint">of {r.calls}</span>
                    {r.calls > 0 && <div className="text-xs text-muted">{pct(r.succeeded / r.calls)}</div>}
                  </td>
                  <td className="px-3 py-2.5 text-right tabular-nums">
                    {usd(r.spend_usd)}
                    {r.spend_missing_calls > 0 && (
                      <div className="mt-1">
                        <Badge tone="warn" title="These calls reported no cost. Their cost is unknown, not $0.">
                          <Icon name="alert" className="size-3" />
                          {r.spend_missing_calls} without a cost
                        </Badge>
                      </div>
                    )}
                  </td>
                  <td className="px-3 py-2.5">
                    <ul className="space-y-1 text-xs">
                      {r.endpoints.map((e) => (
                        <li key={e.endpoint} className="grid grid-cols-[10rem_3.5rem_5rem_auto] items-center gap-2">
                          <span className="truncate" title={e.endpoint}>
                            {endpointTitle.get(e.endpoint) ?? e.endpoint}
                          </span>
                          <span className={cx("tabular-nums", e.success_rate < 0.9 ? "font-semibold text-danger" : "text-muted")} title={`${e.succeeded} of ${e.calls} calls answered`}>
                            {pct(e.success_rate)}
                          </span>
                          <span className="text-muted tabular-nums">{e.cost_per_call == null ? "–" : `${usd(e.cost_per_call)}/call`}</span>
                          {e.missing_cost > 0 ? <span className="text-warn">{e.missing_cost} without a cost</span> : <span />}
                        </li>
                      ))}
                    </ul>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {shown.length === 0 && <p className="px-4 py-8 text-center text-sm text-muted">No check in this range.</p>}
        </div>
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
      <div className="relative min-h-0 flex-1 overflow-auto px-3 py-4 md:px-5 md:py-5">
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
  if (jobs.isPending) return <PageSpinner />;
  if (jobs.isError) return <ErrorNote error={jobs.error} />;
  if (jobs.data.length === 0) return null;
  return (
    <section>
      <h2 className="mb-2 text-sm font-semibold">Recent jobs</h2>
      <Card>
        <ul className="divide-y divide-line text-sm">
          {jobs.data.map((j) => (
            <li key={j.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2">
              <span className="w-28 font-medium">{j.job === "ai_check" ? "AI check" : j.job === "model_refresh" ? "Model lists" : j.job}</span>
              <Badge tone={j.status === "completed" ? "success" : j.status === "failed" || j.status === "refused" ? "danger" : j.status === "skipped" ? "neutral" : "info"}>{j.status}</Badge>
              <span className="text-xs text-muted">{j.trigger.replace("_", " ")}</span>
              {j.run_id && <span className="text-xs text-muted">check #{j.run_id}</span>}
              {j.spend_usd != null && <span className="text-xs text-muted tabular-nums">{usd(j.spend_usd)}</span>}
              <span className="ml-auto text-xs text-muted">
                <RelativeTime iso={j.started_at} />
              </span>
              {j.error && <span className="w-full text-xs text-danger [overflow-wrap:anywhere]">{j.error}</span>}
            </li>
          ))}
        </ul>
      </Card>
    </section>
  );
}
