import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type BrandStanding, type Competitors as Data } from "../../api";
import { Donut, type Segment } from "../../charts";
import { fidelityInfo, lineKey, pct, useLabels } from "../../lines";
import { keys, useAnsweredRuns, useBrands, useSettings, useSurfaces } from "../../queries";
import { Badge, Card, CardHeader, cx, EmptyState, ErrorNote, Pending, ScrollX, useLoading } from "../../ui";
import { useFilter, useView } from "../../view";
import { useOpenEngine } from "./EngineDrawer";
import { ViewHeader } from "./Header";

export function Competitors() {
  const settings = useSettings();
  const runs = useAnsweredRuns();
  const { filter, by, ready } = useFilter();
  const comp = useQuery({ queryKey: keys.competitors(filter, by), queryFn: () => api.metrics.competitors(filter, by), enabled: ready, placeholderData: (p) => p });
  const brand = settings.data?.brand.name || "you";
  useLoading(settings.isPending);

  let body;
  if (!ready || comp.isPending || runs.isPending) body = <Pending />;
  else if (comp.isError) body = <ErrorNote error={comp.error} />;
  else if (!runs.data?.length)
    body = (
      <EmptyState icon="users" title="No checks yet">
        Once a check has run, this shows who gets named instead of {brand}, and where.
      </EmptyState>
    );
  else
    body = (
      <div className={cx("space-y-4", comp.isPlaceholderData && "opacity-60")}>
        <ShareOfVoice data={comp.data} />
        <Heatmap data={comp.data} />
        <WhereYouLose run={runs.data[0].id} />
      </div>
    );
  return (
    <>
      <ViewHeader title="Competitors" subtitle={`Who AI answers name instead of ${brand}, on which surfaces.`} />
      <div className="relative min-h-0 flex-1 overflow-auto px-3 py-4 md:px-5 md:py-5">
        <div className="mx-auto max-w-6xl">{body}</div>
      </div>
    </>
  );
}

/** Your brand is drawn in ink, as everywhere; competitors keep their hue in brand table order whatever their rank. */
function useBrandColor() {
  const brands = useBrands();
  const competitors = (brands.data ?? []).filter((b) => b.kind === "competitor").sort((a, b) => a.id - b.id);
  const slot = new Map(competitors.map((b, i) => [b.id, i + 1]));
  return (b: BrandStanding) => {
    if (b.kind === "self") return "var(--you)";
    const s = slot.get(b.brand_id);
    return s && s <= 8 ? `var(--series-${s})` : "var(--faint)";
  };
}

function ShareOfVoice({ data }: { data: Data }) {
  const color = useBrandColor();
  const self = data.brands.find((b) => b.kind === "self");
  const named = data.brands.filter((b) => b.mentions > 0);
  const top = named.slice(0, 5);
  if (self && self.mentions > 0 && !top.includes(self)) top.push(self);
  const rest = named.filter((b) => !top.includes(b)).reduce((s, b) => s + b.mentions, 0);
  const segments: Segment[] = [...top.map((b) => ({ key: String(b.brand_id), label: `${b.name}: ${pct(b.share)}`, value: b.mentions, color: color(b) })), ...(rest ? [{ key: "other", label: `Other brands: ${pct(rest / data.mentions)}`, value: rest, color: "var(--line-hover)" }] : [])];
  return (
    <Card>
      <CardHeader
        title="Share of voice"
        description={`Each brand's share of the ${data.mentions} mentions of you and your competitors over ${data.runs} ${data.runs === 1 ? "check" : "checks"}. Technologies are left out.`}
      />
      <div className="flex flex-col items-center gap-6 px-4 pb-4 sm:flex-row sm:items-start">
        <Donut segments={segments} label={`Share of voice: ${segments.map((s) => s.label).join(", ")}`}>
          <div>
            {self?.rank ? (
              <>
                <div className="text-xs text-muted">Your rank</div>
                <div className="text-2xl font-semibold">#{self.rank}</div>
                <div className="text-xs text-muted">of {data.brands_named}</div>
              </>
            ) : (
              <div className="max-w-24 text-xs text-muted">You were not named</div>
            )}
          </div>
        </Donut>
        <table className="w-full text-sm">
          <thead className="text-xs text-muted">
            <tr>
              <th className="py-1 text-left font-medium">Rank</th>
              <th className="py-1 text-left font-medium">Brand</th>
              <th className="py-1 text-right font-medium">Mentions</th>
              <th className="py-1 text-right font-medium">Share</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {data.brands.map((b) => (
              <tr key={b.brand_id} className={b.kind === "self" ? "font-semibold" : undefined}>
                <td className="py-1.5 text-muted tabular-nums">{b.rank ? `#${b.rank}` : "–"}</td>
                <td className="py-1.5">
                  <span className="flex items-center gap-2">
                    <span className="size-2.5 shrink-0 rounded-[3px]" style={{ background: top.includes(b) ? color(b) : "var(--line-hover)" }} aria-hidden="true" />
                    {b.name}
                    {b.kind === "self" && <Badge>You</Badge>}
                  </span>
                </td>
                <td className="py-1.5 text-right tabular-nums">{b.mentions}</td>
                <td className="py-1.5 text-right tabular-nums">{pct(b.share)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

/** A rate's step on the sequential ramp in index.css; 0 stays off the ramp so "never named" reads as empty. */
const heatStep = (rate: number) => Math.min(12, Math.max(0, Math.round(rate * 12)));

function Heatmap({ data }: { data: Data }) {
  const openEngine = useOpenEngine();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const [hover, setHover] = useState<string | null>(null);
  const rows = data.brands.filter((b) => b.mentions > 0 || b.kind === "self").slice(0, 12);
  if (data.columns.length === 0) return null;
  return (
    <Card>
      <CardHeader title="Named by surface" description="Each cell is the share of that line's answers naming the brand, on the scale below. Hover or focus a cell for the count, or open a column for that line's detail." />
      <div className="px-4 pb-4">
        <ScrollX label="brands by surface">
          <table className="w-full table-fixed border-separate border-spacing-0.5 text-xs" style={{ minWidth: `calc(7.5rem + ${data.columns.length} * 5.5rem)` }}>
            <colgroup>
              <col className="w-30 md:w-44" />
              {data.columns.map((c) => (
                <col key={lineKey(c)} />
              ))}
            </colgroup>
            <thead>
              <tr>
                <th className="sticky left-0 z-[1] bg-surface" />
                {data.columns.map((c) => (
                  <th key={lineKey(c)} scope="col" className="px-0.5 pb-1 text-left align-bottom font-medium text-muted">
                    <button type="button" onClick={() => openEngine(c)} title={`${c.models.length ? `${c.models.join(", ")}. ` : ""}Open ${labels.line(c.pair, c.models)}`} className="block w-full rounded-control px-1 py-0.5 text-left hover:bg-subtle">
                      <span className="block text-fg underline decoration-line-hover underline-offset-2">{labels.surfaceTitle(c.pair.surface)}</span>
                      {(c.pair.model || c.models.length > 1) && <span className="block font-normal [overflow-wrap:anywhere]">{c.pair.model || `${c.models.length} models`}</span>}
                      <span className={cx("block", c.fidelity === "approximation" ? "text-warn" : "font-normal")}>{fidelityInfo[c.fidelity].label}</span>
                      <span className="block font-normal text-faint">{c.answered} answers</span>
                    </button>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((b) => (
                <tr key={b.brand_id}>
                  <th data-sticky scope="row" className={cx("sticky left-0 z-[1] truncate bg-surface pr-2 text-left", b.kind === "self" ? "font-semibold" : "font-normal text-muted")} title={b.name}>
                    {b.name}
                    {b.kind === "self" && " (you)"}
                  </th>
                  {data.columns.map((c) => {
                    const cell = c.cells.find((x) => x.brand_id === b.brand_id);
                    const rate = cell?.rate ?? 0;
                    const step = heatStep(rate);
                    const k = `${b.brand_id}|${lineKey(c)}`;
                    const tip = `${b.name} on ${labels.line(c.pair, c.models)}: named in ${cell?.named ?? 0} of ${c.answered} answers`;
                    return (
                      <td key={k} className="relative p-0">
                        <div
                          tabIndex={0}
                          aria-label={tip}
                          onPointerEnter={() => setHover(k)}
                          onPointerLeave={() => setHover(null)}
                          onFocus={() => setHover(k)}
                          onBlur={() => setHover(null)}
                          className={cx("grid h-9 place-items-center rounded-inner font-medium tabular-nums outline-none focus-visible:ring-2 focus-visible:ring-ring", hover === k && "ring-2 ring-fg")}
                          style={rate > 0 ? { background: `var(--heat-${step})`, color: `var(--heat-ink-${step})` } : { background: "var(--subtle)" }}
                        >
                          {rate > 0 ? pct(rate) : <span className="text-faint">0</span>}
                        </div>
                        {hover === k && (
                          <div role="status" className="pointer-events-none absolute bottom-full left-1/2 z-10 mb-1 w-max max-w-56 -translate-x-1/2 rounded-surface bg-surface px-2.5 py-1.5 text-xs shadow-floating">
                            <span className="font-semibold">
                              {cell?.named ?? 0} of {c.answered}
                            </span>{" "}
                            <span className="text-muted">
                              answers on {labels.line(c.pair, c.models)} named {b.name}
                            </span>
                          </div>
                        )}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollX>
        <HeatKey />
      </div>
    </Card>
  );
}

function HeatKey() {
  return (
    <div className="mt-2 flex items-center gap-2 text-xs text-faint" aria-hidden="true">
      0%
      <span className="flex h-2 w-32 overflow-hidden rounded-full">
        {Array.from({ length: 13 }, (_, i) => (
          <span key={i} className="flex-1" style={{ background: `var(--heat-${i})` }} />
        ))}
      </span>
      100%
    </div>
  );
}

function WhereYouLose({ run }: { run: number }) {
  const view = useView();
  const gaps = useQuery({ queryKey: keys.gaps(run), queryFn: () => api.gaps(run) });
  if (gaps.isPending) return <Pending />;
  if (gaps.isError) return <ErrorNote error={gaps.error} />;
  const losses = gaps.data.gaps.filter((g) => g.named_on_miss.length > 0);
  return (
    <Card>
      <CardHeader title="Where you lose" description={`Questions in check #${gaps.data.run_id} where a surface and model named a competitor but not you, with who was named instead. Covers every surface.`} />
      {losses.length === 0 ? (
        <p className="px-4 pb-6 text-sm text-muted">No answer in the latest check named a competitor without naming you.</p>
      ) : (
        <ul className="divide-y divide-line border-t border-line">
          {losses.map((g) => (
            <li key={g.question.id}>
              <button type="button" onClick={() => view.set({ q: g.question.id, qa: null }, { push: true })} className="flex w-full flex-col gap-1.5 px-4 py-3 text-left hover:bg-subtle sm:flex-row sm:items-center sm:gap-4">
                <span className="min-w-0 flex-1">
                  <span className="block font-medium">{g.question.text}</span>
                  <span className="block text-xs text-faint">{g.question.group}</span>
                </span>
                <span className="shrink-0 text-xs text-muted tabular-nums">
                  Missed by {g.engines - g.named} of {g.engines} pairs
                </span>
                <span className="flex shrink-0 flex-wrap gap-1">
                  {g.named_on_miss.map((n) => (
                    <Badge key={n} tone="outline">
                      {n}
                    </Badge>
                  ))}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
