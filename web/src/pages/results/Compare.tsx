import { useQuery } from "@tanstack/react-query";
import { api, type Outcome, type PairChange, type Standing } from "../../api";
import { chance, pct, shortDay, useLabels } from "../../lines";
import { keys, useAnsweredRuns, useSettings, useSurfaces } from "../../queries";
import { nextCheckDay } from "../../checks";
import { Link } from "react-router";
import { buttonClass, Card, CardHeader, cx, EmptyState, ErrorNote, Icon, Pending, scrollArea, selectClass, useLoading } from "../../ui";
import { runTime, useView } from "../../view";
import { Segmented, ViewHeader } from "./Header";
import { NamedOf, outcomeMark } from "./QuestionsTable";

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
  const mark = s.mentioned ? (
    <span className="font-semibold text-success tabular-nums">#{s.position}</span>
  ) : (
    <span className="whitespace-nowrap text-muted">{s.cited ? "Linked, not named" : "Not named"}</span>
  );
  if (s.answered <= 1) return mark;
  return (
    <span className="inline-flex flex-wrap items-baseline gap-x-1">
      {mark}
      <NamedOf c={s} />
    </span>
  );
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

const rowGrid = "grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-0.5 md:grid-cols-[minmax(0,1fr)_8rem_8rem_8rem] md:items-center lg:grid-cols-[minmax(0,1fr)_10.5rem_10.5rem_8rem]";

/** AI answers > What changed: every question and AI app that gained, lost or moved between two checks. */
export function Compare() {
  const view = useView();
  const runs = useAnsweredRuns();
  const settings = useSettings();
  useLoading(settings.isPending);
  const next = settings.data?.next_check;
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
      <EmptyState
        icon="layers"
        title={list.length === 0 ? "No checks yet" : "One check so far"}
        action={
          list.length === 1 ? (
            <Link to={`/ai/questions${view.filterSearch()}`} className={buttonClass()}>
              Read the answers
            </Link>
          ) : undefined
        }
      >
        What changed needs two checks to compare.{" "}
        {next ? `Your next one runs ${nextCheckDay(next)}.` : settings.data?.checks_paused ? "Scheduled checks are paused." : ""}
        {list.length === 1 && " Until then, read the answers on Questions."}
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
    const flipped = cmp.data != null && cmp.data.counts.gained + cmp.data.counts.lost > 0;
    const sampled = cmp.data?.pairs.some((p) => (p.before?.answer_ids.length ?? 0) > 1 || (p.after?.answer_ids.length ?? 0) > 1) ?? false;
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
                {flipped && (
                  <div className={cx("text-xs", cmp.data.move.real ? "font-medium" : "text-muted")} data-verdict={cmp.data.move.real ? "real" : "noise"}>
                    {cmp.data.move.real ? `A real move, ${pct(cmp.data.move.before_rate)} to ${pct(cmp.data.move.after_rate)}` : "Within chance, may be noise"}
                  </div>
                )}
              </Card>
            </div>
            <p className="text-xs text-muted">
              Gained: an AI app named you and did not before. Lost: the reverse. Moved: it named you both times, in a different position. Net change is gained minus lost.
              {sampled && " An AI app asked a question more than once counts as naming you when most of its answers did."}
              {flipped && ` Chance alone gives a move this large ${chance(cmp.data.move.p_value)}. A move is real when that is less than 5 times in 100.`}
            </p>
            <Card>
              <CardHeader
                title={
                  <>
                    Questions and AI apps{" "}
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
                  Each AI app shows where you stood in check #{cmp.data.before}, then in check #{cmp.data.after}.
                </p>
              )}
              {pairs.length > 0 && (
                <div className={cx(rowGrid, "hidden border-y border-line px-4 py-2 text-xs font-medium text-muted md:grid")} aria-hidden="true">
                  <span>Question, then each AI app</span>
                  <span>Check #{cmp.data.before}</span>
                  <span>Check #{cmp.data.after}</span>
                  <span>What changed</span>
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
                      <ul aria-label={`AI apps for ${q.question.text}`}>
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
      <ViewHeader
        title="What changed"
        range={false}
        scope={false}
      />
      <div className={cx("relative min-h-0 flex-1 px-3 py-4 md:px-5 md:py-5", scrollArea)}>
        <div className="mx-auto max-w-6xl">{body}</div>
      </div>
    </>
  );
}
