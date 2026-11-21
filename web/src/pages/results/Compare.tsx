import { useQuery } from "@tanstack/react-query";
import { api, type Outcome, type Standing } from "../../api";
import { shortDay, useLabels } from "../../lines";
import { keys, useAnsweredRuns, useSurfaces } from "../../queries";
import { Card, cx, EmptyState, ErrorNote, Icon, PageSpinner, selectClass } from "../../ui";
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

export function Compare() {
  const view = useView();
  const runs = useAnsweredRuns();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const list = runs.data ?? [];
  const after = Number(view.get("after")) || list[0]?.id || 0;
  const before = Number(view.get("before")) || list.find((r) => r.id < after)?.id || 0;
  const cmp = useQuery({ queryKey: keys.compare(before, after), queryFn: () => api.compare(before, after), enabled: before > 0 && after > 0 && before < after, placeholderData: (p) => p });
  const show = view.get("show") || "changed";

  let body;
  if (runs.isPending) body = <PageSpinner />;
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
        {cmp.isPending && <PageSpinner />}
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
              <div className="flex flex-wrap items-center gap-2 px-4 py-3">
                <h2 className="flex-1 text-sm font-semibold">
                  Questions and pairs{" "}
                  <span className="font-normal text-muted">
                    ({pairs.length} of {cmp.data.pairs.length})
                  </span>
                </h2>
                <Segmented size="sm" label="Show" value={show} options={shows.map((s) => ({ id: s.id, label: s.label }))} onChange={(v) => view.set({ show: v })} />
              </div>
              <div className="overflow-x-auto">
                <table className="w-full min-w-max text-sm">
                  <thead className="border-y border-line text-xs text-muted">
                    <tr>
                      <th className="px-4 py-2 text-left font-medium">Question</th>
                      <th className="px-3 py-2 text-left font-medium">Surface and model</th>
                      <th className="px-3 py-2 text-left font-medium">#{cmp.data.before}</th>
                      <th className="px-3 py-2 text-left font-medium">#{cmp.data.after}</th>
                      <th className="px-3 py-2 text-left font-medium">Change</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line">
                    {pairs.map((p) => (
                      <tr key={`${p.question_id}-${p.pair_id}`} className="cursor-pointer hover:bg-subtle" onClick={() => view.set({ q: p.question_id, qa: p.after?.answer_id ?? p.before?.answer_id ?? null }, { push: true })}>
                        <td className="max-w-md px-4 py-2">
                          <div className="truncate font-medium" title={p.question.text}>
                            {p.question.text}
                          </div>
                          <div className="text-xs text-faint">{p.question.group}</div>
                        </td>
                        <td className="px-3 py-2 text-xs whitespace-nowrap">{labels.line(p.pair)}</td>
                        <td className="px-3 py-2 text-xs whitespace-nowrap">
                          <StandingMark s={p.before} />
                        </td>
                        <td className="px-3 py-2 text-xs whitespace-nowrap">
                          <StandingMark s={p.after} />
                        </td>
                        <td className={cx("px-3 py-2 text-xs font-medium whitespace-nowrap", outcomeMark[p.outcome].className)}>
                          <span className="inline-flex items-center gap-1">
                            {outcomeMark[p.outcome].icon && <Icon name={outcomeMark[p.outcome].icon!} className="size-3" />}
                            {p.outcome === "not_comparable" ? "Not comparable" : outcomeMark[p.outcome].label}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {pairs.length === 0 && <p className="px-4 py-8 text-center text-sm text-muted">Nothing {show === "all" ? "" : show} between these checks.</p>}
              </div>
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
