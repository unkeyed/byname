import { useQuery } from "@tanstack/react-query";
import { api, type Outcome, type PairChange, type Standing } from "../../api";
import { shortDay, useLabels } from "../../lines";
import { keys, useAnsweredRuns, useSurfaces } from "../../queries";
import { Card, CardHeader, cx, EmptyState, ErrorNote, Icon, Pending, selectClass } from "../../ui";
import { runTime, useView } from "../../view";
import { Segmented, ViewHeader } from "./Header";
import { outcomeMark } from "./QuestionsTable";

const shows = [
  { id: "changed", label: "Changed" },
  { id: "gained", label: "Gained" },
  { id: "lost", label: "Lost" },
  { id: "moved", label: "Moved" },
  { id: "all", label: "All" },
] as const;

const order: Outcome[] = ["gained", "lost", "moved", "unchanged", "not_comparable"];

function StandingMark({ s }: { s: Standing | null }) {
  if (!s) return <span className="text-faint">Not asked</span>;
  if (s.status === "failed") return <span className="text-faint">Failed</span>;
  if (s.status === "no_answer") return <span className="text-faint">No answer</span>;
  if (s.mentioned) return <span className="font-semibold text-success tabular-nums">#{s.position}</span>;
  return <span className="text-muted">{s.cited ? "Cited, not named" : "Not named"}</span>;
}

/** Places climbed: positive is up the list (#3 to #1), negative is down it. */
function climbed(p: PairChange): number {
  const [b, a] = [p.before?.position, p.after?.position];
  return p.outcome === "moved" && b != null && a != null ? b - a : 0;
}

/** Gained and lost read as they are; a move says which way it went, since #1 to #2 is a drop. */
function ChangeMark({ p }: { p: PairChange }) {
  const by = climbed(p);
  if (by !== 0)
    return (
      <span className={cx("inline-flex items-center gap-1 text-xs font-medium whitespace-nowrap", by > 0 ? "text-success" : "text-danger")}>
        <Icon name={by > 0 ? "up" : "down"} className="size-3" />
        {by > 0 ? "Up" : "Down"} {Math.abs(by)}
      </span>
    );
  const o = outcomeMark[p.outcome];
  return (
    <span className={cx("inline-flex items-center gap-1 text-xs font-medium whitespace-nowrap", o.className)}>
      {o.icon && <Icon name={o.icon} className="size-3" />}
      {p.outcome === "not_comparable" ? "Not comparable" : o.label}
    </span>
  );
}

const rowGrid = "grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-0.5 md:grid-cols-[minmax(0,1fr)_8rem_8rem_8rem] md:items-center";

export function Compare() {
  const view = useView();
  const runs = useAnsweredRuns();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const list = runs.data ?? [];
  const after = Number(view.get("after")) || list[0]?.id || 0;
  const before = Number(view.get("before")) || list.find((r) => r.id < after)?.id || 0;
  const cmp = useQuery({ queryKey: keys.compare(before, after), queryFn: () => api.compare(before, after), enabled: before > 0 && after > 0 && before < after });
  const show = view.get("show") || "changed";

  let body;
  if (runs.isPending) body = <Pending />;
  else if (runs.isError) body = <ErrorNote error={runs.error} />;
  else if (list.length < 2)
    body = (
      <EmptyState icon="layers" title="Two checks needed">
        Compare shows what changed between two checks. {list.length === 0 ? "No check has run yet." : "Only one check has run so far."}
      </EmptyState>
    );
  else {
    const label = (id: number) => {
      const r = list.find((x) => x.id === id);
      return r ? `Check #${r.id}, ${shortDay(runTime(r))}` : `Check #${id}`;
    };
    const pairs = cmp.data?.pairs.filter((p) => (show === "all" ? true : show === "changed" ? ["gained", "lost", "moved"].includes(p.outcome) : p.outcome === show)) ?? [];
    const byQuestion = new Map<number, PairChange[]>();
    for (const p of pairs) byQuestion.set(p.question_id, [...(byQuestion.get(p.question_id) ?? []), p]);
    const moves = (cmp.data?.pairs ?? []).map(climbed);
    const openPair = (p: PairChange) => view.set({ q: p.question_id, qa: p.after?.answer_id ?? p.before?.answer_id ?? null }, { push: true });
    body = (
      <div className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <select aria-label="Before" className={selectClass} value={before} onChange={(e) => view.set({ before: e.target.value })}>
            {list
              .filter((r) => r.id < after)
              .map((r) => (
                <option key={r.id} value={r.id}>
                  {label(r.id)}
                </option>
              ))}
          </select>
          <Icon name="right" className="size-4 text-faint" />
          <select aria-label="After" className={selectClass} value={after} onChange={(e) => view.set({ after: e.target.value, before: Number(e.target.value) <= before ? null : before })}>
            {list
              .filter((r) => r.id > list[list.length - 1].id)
              .map((r) => (
                <option key={r.id} value={r.id}>
                  {label(r.id)}
                </option>
              ))}
          </select>
        </div>
        {cmp.isError && <ErrorNote error={cmp.error} />}
        {cmp.isPending && <Pending />}
        {cmp.data && (
          <>
            <div className={cx("grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6", cmp.isPlaceholderData && "opacity-60")} aria-label="Counts">
              {order.map((o) => (
                <Card key={o} className="px-4 py-3">
                  <div className={cx("flex items-center gap-1 text-xs font-medium", outcomeMark[o].className)}>
                    {outcomeMark[o].icon && <Icon name={outcomeMark[o].icon!} className="size-3" />}
                    <span className={o === "unchanged" || o === "not_comparable" ? "text-muted" : undefined}>{o === "not_comparable" ? "Not comparable" : outcomeMark[o].label}</span>
                  </div>
                  <div className="text-2xl font-semibold tabular-nums" data-count={o}>
                    {cmp.data.counts[o]}
                  </div>
                  {o === "moved" && cmp.data.counts.moved > 0 && (
                    <div className="text-xs text-muted tabular-nums">
                      {moves.filter((m) => m > 0).length} up, {moves.filter((m) => m < 0).length} down
                    </div>
                  )}
                </Card>
              ))}
              <Card className="px-4 py-3">
                <div className="text-xs font-medium text-muted">Net change</div>
                <div className={cx("text-2xl font-semibold tabular-nums", cmp.data.net > 0 ? "text-success" : cmp.data.net < 0 ? "text-danger" : undefined)} data-count="net">
                  {cmp.data.net > 0 ? `+${cmp.data.net}` : cmp.data.net}
                </div>
              </Card>
            </div>
            <Card>
              <CardHeader
                title={
                  <>
                    Questions and pairs{" "}
                    <span className="font-normal text-muted">
                      ({pairs.length} of {cmp.data.pairs.length})
                    </span>
                  </>
                }
              >
                <Segmented size="sm" label="Show" value={show} options={shows.map((s) => ({ id: s.id, label: s.label }))} onChange={(v) => view.set({ show: v })} />
              </CardHeader>
              {pairs.length > 0 && (
                <p className="border-t border-line px-4 py-2 text-xs text-muted md:hidden">
                  Each pair shows where you stood in check #{cmp.data.before}, then in check #{cmp.data.after}.
                </p>
              )}
              {pairs.length > 0 && (
                <div className={cx(rowGrid, "hidden border-y border-line px-4 py-2 text-xs font-medium text-muted md:grid")} aria-hidden="true">
                  <span>Question, then each surface and model</span>
                  <span>Check #{cmp.data.before}</span>
                  <span>Check #{cmp.data.after}</span>
                  <span>Change</span>
                </div>
              )}
              <ul className="divide-y divide-line border-t border-line md:border-t-0">
                {[...byQuestion.values()].map((group) => {
                  const q = group[0];
                  return (
                    <li key={q.question_id} className="py-2">
                      <button type="button" onClick={() => openPair(q)} className="block w-full px-4 py-1 text-left hover:underline">
                        <span className="block font-medium">{q.question.text}</span>
                        <span className="block text-xs text-faint">{q.question.group}</span>
                      </button>
                      <ul aria-label={`Pairs for ${q.question.text}`}>
                        {group.map((p) => (
                          <li key={p.pair_id}>
                            <button type="button" onClick={() => openPair(p)} className={cx(rowGrid, "w-full px-4 py-1.5 text-left text-xs hover:bg-subtle")}>
                              <span className="min-w-0 text-muted md:col-start-1 md:row-start-1">{labels.line(p.pair)}</span>
                              <span className="col-span-2 row-start-2 flex items-center gap-1.5 md:contents">
                                <span className="md:col-start-2 md:row-start-1">
                                  <StandingMark s={p.before} />
                                </span>
                                <Icon name="right" className="size-3 text-faint md:hidden" />
                                <span className="md:col-start-3 md:row-start-1">
                                  <StandingMark s={p.after} />
                                </span>
                              </span>
                              <span className="col-start-2 row-start-1 text-right md:col-start-4 md:text-left">
                                <ChangeMark p={p} />
                              </span>
                            </button>
                          </li>
                        ))}
                      </ul>
                    </li>
                  );
                })}
              </ul>
              {pairs.length === 0 && <p className="px-4 py-8 text-center text-sm text-muted">Nothing {show === "all" ? "" : show} between these checks.</p>}
            </Card>
          </>
        )}
      </div>
    );
  }

  return (
    <>
      <ViewHeader title="Compare" subtitle="Pick two checks to see every question and pair you gained, lost or moved on, and the net change." range={false} scope={false} />
      <div className="relative min-h-0 flex-1 overflow-auto px-3 py-4 md:px-5 md:py-5">
        <div className="mx-auto max-w-6xl">{body}</div>
      </div>
    </>
  );
}
