import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, type Grouping, type ResultsFilter, type Run, type RunSummary, type Series } from "../../api";
import { LineChart, LineKey, Sparkline, type ChartLine } from "../../charts";
import { fidelityInfo, lineKey, palette, pct, shortDay, usd, useLabels } from "../../lines";
import { keys, useAnsweredRuns, useRuns, useSettings, useSurfaces } from "../../queries";
import { Badge, Card, CardHeader, cx, EmptyState, ErrorNote, Icon, PageSpinner, RelativeTime, ScrollX } from "../../ui";
import { runTime, useFilter, useView } from "../../view";
import { useOpenEngine } from "./EngineDrawer";
import { Segmented, ViewHeader } from "./Header";
import { QuestionsTable } from "./QuestionsTable";
import { RunNowButton, RunStatusBadge, useCanRun } from "./RunNow";

export function Overview() {
  const settings = useSettings();
  const runs = useRuns();
  const answered = useAnsweredRuns();
  const canRun = useCanRun();
  const { filter, by, ready } = useFilter();
  const brand = settings.data?.brand.name || "your brand";

  let body;
  if (runs.isPending || !ready) body = <PageSpinner />;
  else if (runs.isError) body = <ErrorNote error={runs.error} />;
  else if (!answered.data?.length)
    body = (
      <EmptyState icon="chart" title="No checks yet" action={canRun ? <RunNowButton /> : undefined}>
        {runs.data.length > 0
          ? "A check is under way. Results appear here when its first answers are stored."
          : canRun
            ? `Run a check to ask every question on every surface and see how often ${brand} is named.`
            : `Nothing to show until someone with the Editor or Admin role runs a check.`}
      </EmptyState>
    );
  else
    body = (
      <div className="space-y-4">
        <StatusStrip runs={runs.data} />
        <Tiles filter={filter} />
        <RatesCard filter={filter} by={by} />
        <ShareCard filter={filter} />
        <Card>
          <CardHeader title="Questions" description="Each question on each surface and model in the latest check. Open one to see its history and answers." />
          <QuestionsTable run={answered.data[0]} filter={filter} />
        </Card>
      </div>
    );

  return (
    <>
      <ViewHeader title="Overview" subtitle={`How often AI answers name ${brand}, and how that moves from check to check.`} action={canRun && answered.data?.length ? <RunNowButton variant="secondary" /> : undefined} />
      <div className="relative min-h-0 flex-1 overflow-auto px-3 py-4 md:px-5 md:py-5">
        <div className="mx-auto max-w-6xl">{body}</div>
      </div>
    </>
  );
}

function StatusStrip({ runs }: { runs: Run[] }) {
  const settings = useSettings();
  const last = runs[0];
  const s = settings.data;
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-2 rounded-surface border border-line bg-surface px-4 py-2.5 text-xs text-muted" aria-label="Run status">
      <span className="flex items-center gap-2">
        Last check
        <Link to={`/runs`} className="font-medium text-fg hover:underline">
          #{last.id}
        </Link>
        <RunStatusBadge run={last} />
        <RelativeTime iso={last.finished_at ?? runTime(last)} />
      </span>
      <span>
        Calls answered{" "}
        <span className="font-medium text-fg tabular-nums">
          {last.succeeded} of {last.calls}
        </span>
      </span>
      <span className="flex items-center gap-2">
        Spend <span className="font-medium text-fg tabular-nums">{usd(last.spend_usd)}</span>
        {last.spend_missing_calls > 0 && (
          <Badge tone="warn" title="These calls reported no cost. Their cost is unknown, not $0, so the real spend is higher.">
            <Icon name="alert" className="size-3" />
            {last.spend_missing_calls} without a cost
          </Badge>
        )}
      </span>
      <span className="flex items-center gap-1.5">
        Next check{" "}
        {s?.checks_paused ? (
          <Badge>Paused</Badge>
        ) : s?.next_check_at ? (
          <span className="font-medium text-fg">
            <RelativeTime iso={s.next_check_at} />
          </span>
        ) : (
          <span className="text-faint">not scheduled</span>
        )}
      </span>
    </div>
  );
}

function Delta({ now, before, unit = "pts" }: { now: number | null; before: number | null | undefined; unit?: string }) {
  if (now == null || before == null) return null;
  const d = Math.round((now - before) * 100);
  if (d === 0) return <span className="text-xs text-faint">No change since the previous check</span>;
  return (
    <span className="text-xs text-muted">
      <span className={cx("inline-flex items-center gap-0.5 font-medium", d > 0 ? "text-success" : "text-danger")}>
        <Icon name={d > 0 ? "up" : "down"} className="size-3" />
        {Math.abs(d)} {unit} {d > 0 ? "up" : "down"}
      </span>
      <span className="sr-only sm:not-sr-only"> since the previous check</span>
    </span>
  );
}

function Tile({ label, value, detail, delta, trend }: { label: string; value: string; detail: string; delta?: React.ReactNode; trend?: (number | null)[] }) {
  return (
    <Card className="flex flex-col gap-1 px-3 py-3 sm:px-4">
      <div className="flex flex-wrap items-start justify-between gap-x-2 gap-y-1">
        <h3 className="text-xs font-medium text-muted">{label}</h3>
        {trend && trend.length > 1 && <Sparkline values={trend} className="hidden sm:block" />}
      </div>
      <div className="flex items-end justify-between gap-2">
        <span className="text-2xl font-semibold">{value}</span>
        {trend && trend.length > 1 && <Sparkline values={trend} className="mb-1 h-5 w-16 sm:hidden" />}
      </div>
      <div className="text-xs text-muted">{detail}</div>
      <div className="min-h-4">{delta}</div>
    </Card>
  );
}

function Tiles({ filter }: { filter: ResultsFilter }) {
  const summary = useQuery({ queryKey: keys.summary(filter), queryFn: () => api.metrics.summary(filter) });
  if (summary.isPending) return <PageSpinner />;
  if (summary.isError) return <ErrorNote error={summary.error} />;
  const all: RunSummary[] = summary.data;
  const cur = all[all.length - 1];
  const prev = all[all.length - 2];
  return (
    <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
      {cur ? (
        <>
          <Tile label="Mention rate" value={pct(cur.mention_rate)} detail={`${cur.mentioned} of ${cur.answered} answers named you`} delta={<Delta now={cur.mention_rate} before={prev?.mention_rate} />} trend={all.map((r) => r.mention_rate)} />
          <Tile label="Citation rate" value={pct(cur.citation_rate)} detail={`${cur.cited} of ${cur.answered} answers cited your domains`} delta={<Delta now={cur.citation_rate} before={prev?.citation_rate} />} trend={all.map((r) => r.citation_rate)} />
          <Tile label="Share of mentions" value={pct(cur.share)} detail={`Of ${cur.mentions} mentions of you and your competitors`} delta={<Delta now={cur.share} before={prev?.share} />} trend={all.map((r) => r.share)} />
        </>
      ) : (
        <Card className="col-span-2 px-4 py-3 text-sm text-muted xl:col-span-3">No check in this range. Widen the date range to see results.</Card>
      )}
      <Card className="flex flex-col gap-1 px-3 py-3 sm:px-4">
        <h3 className="text-xs font-medium text-muted">Search Console clicks, 28 days</h3>
        <p className="text-sm text-muted">Not connected</p>
        <Link to="/search" className="mt-auto inline-flex items-center gap-1 pt-1 text-xs font-medium text-fg hover:underline">
          <Icon name="plug" className="size-3.5" />
          Connect
        </Link>
      </Card>
    </div>
  );
}

function RatesCard({ filter, by }: { filter: ResultsFilter; by: Grouping }) {
  const view = useView();
  const openEngine = useOpenEngine();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const color = palette(surfaces.data);
  const metric = view.get("metric") === "citation" ? "citation" : "mention";
  const rates = useQuery({ queryKey: keys.rates(filter, by), queryFn: () => api.metrics.rates(filter, by), placeholderData: (prev) => prev });

  let body;
  if (rates.isPending) body = <PageSpinner />;
  else if (rates.isError) body = <ErrorNote error={rates.error} />;
  else {
    const runIDs = [...new Set(rates.data.flatMap((s) => s.points.map((p) => p.run_id)))].sort((a, b) => a - b);
    const at = new Map(rates.data.flatMap((s) => s.points.map((p) => [p.run_id, p.run_started_at] as const)));
    const xs = runIDs.map((id) => ({ key: String(id), label: `#${id}`, title: `Check #${id}, ${shortDay(at.get(id)!)}` }));
    const lines: (ChartLine & { fidelity: keyof typeof fidelityInfo; latest: number | null; models: string[]; series: Series })[] = rates.data.map((s) => {
      const byRun = new Map(s.points.map((p) => [p.run_id, metric === "mention" ? p.mention_rate : p.citation_rate]));
      const values = runIDs.map((id) => byRun.get(id) ?? null);
      return {
        key: lineKey(s),
        label: labels.line(s.pair, s.models),
        color: color(s.pair),
        approx: s.fidelity === "approximation",
        values,
        fidelity: s.fidelity,
        latest: values.reduce<number | null>((l, v) => (v == null ? l : v), null),
        models: s.models,
        series: s,
      };
    });
    body =
      lines.length === 0 ? (
        <p className="px-4 py-10 text-center text-sm text-muted">No answers in this range.</p>
      ) : (
        <div className={cx("grid grid-cols-[minmax(0,1fr)] gap-4 px-4 pb-4 lg:grid-cols-[minmax(0,1fr)_16rem]", rates.isPlaceholderData && "opacity-60")}>
          <LineChart xs={xs} lines={lines} format={(v) => pct(v)} label={`${metric === "mention" ? "Mention" : "Citation"} rate per check, one line per ${by === "surface" ? "surface" : "surface and model"}`} />
          <ul aria-label="Lines, with each line's latest value. Open one for its detail." className="-mx-1.5 space-y-0.5 self-start text-xs">
            {lines.map((l) => (
              <li key={l.key}>
                <button type="button" onClick={() => openEngine(l.series)} title={`${l.models.length ? `${l.models.join(", ")}. ` : ""}Open ${l.label}`} className="flex w-full items-start gap-2 rounded-control px-1.5 py-1 text-left hover:bg-subtle">
                  <LineKey color={l.color} dashed={l.approx} className="mt-1.5" />
                  <span className="min-w-0 flex-1">
                    <span className="block font-medium [overflow-wrap:anywhere]">{l.label}</span>
                    <span className={cx("block", l.approx ? "text-warn" : "text-faint")}>{fidelityInfo[l.fidelity].label}</span>
                  </span>
                  <span className="text-sm font-semibold tabular-nums">{pct(l.latest)}</span>
                  <Icon name="right" className="mt-0.5 size-3.5 text-faint" />
                </button>
              </li>
            ))}
          </ul>
          <details className="text-xs lg:col-span-2">
            <summary className="cursor-pointer text-muted hover:text-fg">Show as a table</summary>
            <div className="mt-2">
              <ScrollX label="rates table">
                <table className="w-full min-w-max text-left tabular-nums">
                  <thead className="text-muted">
                    <tr>
                      <th className="sticky left-0 bg-surface py-1 pr-4 font-medium">Line</th>
                      {xs.map((x) => (
                        <th key={x.key} className="py-1 pr-4 font-medium" title={x.title}>
                          {x.label}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line">
                    {lines.map((l) => (
                      <tr key={l.key}>
                        <td data-sticky className="sticky left-0 max-w-48 bg-surface py-1 pr-4">
                          {l.label} <span className="text-faint">({fidelityInfo[l.fidelity].label})</span>
                        </td>
                        {l.values.map((v, i) => (
                          <td key={i} className="py-1 pr-4">
                            {pct(v)}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </ScrollX>
            </div>
          </details>
        </div>
      );
  }

  return (
    <Card>
      <CardHeader title={`${metric === "mention" ? "Mention rate" : "Citation rate"} by check`} description="Lines of different fidelity are never combined. Approximations are dashed. Open a line for its detail.">
        <Segmented size="sm" label="Lines" value={by === "surface" ? "surface" : "model"} options={[{ id: "surface", label: "By surface" }, { id: "model", label: "By model" }]} onChange={(v) => view.set(v === "model" ? { by: "model" } : { by: null, model: null })} />
        <Segmented size="sm" label="Metric" value={metric} options={[{ id: "mention", label: "Mentions" }, { id: "citation", label: "Citations" }]} onChange={(v) => view.set({ metric: v })} />
      </CardHeader>
      {body}
    </Card>
  );
}

function ShareCard({ filter }: { filter: ResultsFilter }) {
  const view = useView();
  const comp = useQuery({ queryKey: keys.competitors(filter, "surface"), queryFn: () => api.metrics.competitors(filter, "surface") });
  if (comp.isPending) return <PageSpinner />;
  if (comp.isError) return <ErrorNote error={comp.error} />;
  const self = comp.data.brands.find((b) => b.kind === "self");
  const top = comp.data.brands.filter((b) => b.mentions > 0).slice(0, 6);
  if (self && !top.includes(self)) top.push(self);
  const max = Math.max(...top.map((b) => b.share), 0.01);
  return (
    <Card>
      <CardHeader title="Share of mentions against competitors" description={`${comp.data.mentions} mentions over ${comp.data.runs} ${comp.data.runs === 1 ? "check" : "checks"}. Technologies are left out.`}>
        <Link to={`/competitors${view.filterSearch()}`} className="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-muted hover:text-fg">
          Competitors <Icon name="right" className="size-3" />
        </Link>
      </CardHeader>
      {top.length === 0 ? (
        <p className="px-4 pb-6 text-sm text-muted">No brand was named in this range.</p>
      ) : (
        <ul className="space-y-2 px-4 pb-4">
          {top.map((b) => (
            <li key={b.brand_id} className="grid grid-cols-[minmax(0,1fr)_3rem] items-center gap-x-3 gap-y-1 text-xs sm:grid-cols-[minmax(6rem,10rem)_minmax(0,1fr)_3rem]">
              <span className={cx("truncate", b.kind === "self" ? "font-semibold" : "text-muted")}>
                {b.name}
                {b.kind === "self" && " (you)"}
              </span>
              <span className="col-span-2 row-start-2 h-2 rounded-full bg-subtle sm:col-span-1 sm:row-start-auto">
                <span className="block h-2 rounded-full" style={{ width: `${(b.share / max) * 100}%`, background: b.kind === "self" ? "var(--you)" : "var(--line-hover)" }} />
              </span>
              <span className="col-start-2 row-start-1 text-right font-medium tabular-nums sm:col-start-auto sm:row-start-auto">{pct(b.share)}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
