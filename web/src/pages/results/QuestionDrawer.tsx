import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api, type Answer, type BrandEntry, type HistoryPoint, type HistorySeries } from "../../api";
import { fidelityInfo, lineKey, shortDay, useLabels } from "../../lines";
import { money } from "../../money";
import { keys, useBrands, useSettings, useSurfaces } from "../../queries";
import { Markdown } from "../../markdown";
import { Badge, Button, cx, ErrorNote, Icon, LoadBoundary, Pending, ScrollX, selectClass, useModal } from "../../ui";
import { useFilter, useView } from "../../view";
import { PageMark } from "./Missed";
import { Segmented } from "./Header";
import { CountAnyway, LeftOutBadge, leftOutWhy } from "./Scoring";

/** The question view: open wherever ?q= names a question, with ?qa= naming the answer shown. */
export function QuestionDrawer() {
  const view = useView();
  const q = Number(view.get("q")) || 0;
  const ref = useModal(q > 0);
  const close = () => view.set({ q: null, qa: null, qv: null });
  return (
    <dialog
      ref={ref}
      onClose={close}
      onClick={(e) => e.target === ref.current && close()}
      aria-label="Question"
      data-drawer
      className="m-0 ml-auto h-dvh max-h-none w-full max-w-3xl border-0 border-l border-line bg-bg p-0 text-fg shadow-floating"
    >
      {q > 0 && (
        <LoadBoundary key={q} className="h-full">
          <Body question={q} onClose={close} />
        </LoadBoundary>
      )}
    </dialog>
  );
}

function Mark({ point, onClick, selected }: { point: HistoryPoint | undefined; onClick?: () => void; selected?: boolean }) {
  let body: ReactNode;
  let title: string;
  if (!point)
    return (
      <span className="block px-1.5 text-center text-xs text-faint" title="Not asked in this check">
        ·
      </span>
    );
  if (point.status === "failed") [body, title] = [<span className="text-faint">Failed</span>, "The call failed"];
  else if (point.status === "no_answer") [body, title] = [<span className="text-faint">No answer</span>, "This AI app showed no AI answer"];
  else if (point.mentioned) [body, title] = [<span className="font-semibold text-success">#{point.position ?? "?"}</span>, `Named you at #${point.position}`];
  else if (point.cited) [body, title] = [<span className="text-info">Linked</span>, "Linked to your site without naming you"];
  else [body, title] = [<span className="text-faint">—</span>, "Did not name you"];
  if (!onClick)
    return (
      <span title={title} className="rounded-inner px-1.5 py-0.5 text-xs tabular-nums">
        {body}
      </span>
    );
  return (
    <button type="button" onClick={onClick} title={`${title}. Show this answer.`} className={cx("w-full rounded-inner px-1.5 py-3.5 text-center md:py-0.5 text-xs whitespace-nowrap tabular-nums hover:bg-subtle-strong", selected && "bg-subtle-strong ring-1 ring-line-hover")}>
      {body}
    </button>
  );
}

function Body({ question, onClose }: { question: number; onClose: () => void }) {
  const view = useView();
  const { filter } = useFilter();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const history = useQuery({ queryKey: keys.history(question, filter.since), queryFn: () => api.history(question, filter.since) });

  if (history.isPending) return <Pending />;
  if (history.isError)
    return (
      <div className="space-y-3 p-4">
        <ErrorNote error={history.error} />
        <Button onClick={onClose}>Close</Button>
      </div>
    );
  const series = history.data.series.filter((s) => (!filter.surface || s.pair.surface === filter.surface) && (!filter.model || s.pair.model === filter.model));
  const runs = [...new Map(series.flatMap((s) => s.points.map((p) => [p.run_id, p.run_started_at] as const))).entries()].sort((a, b) => a[0] - b[0]);
  const qa = Number(view.get("qa")) || 0;
  const raw = view.get("qv") === "raw";
  const selSeries = series.find((s) => s.points.some((p) => p.answer_id === qa)) ?? series[0];
  const selPoint = selSeries?.points.find((p) => p.answer_id === qa) ?? selSeries?.points[selSeries.points.length - 1];
  const pick = (answer: number, patch: { qv?: string | null } = {}) => view.set({ qa: answer, ...patch });

  return (
    <div className="flex h-full flex-col">
      <div className="flex shrink-0 items-start gap-3 border-b border-line px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="text-xs text-muted">{history.data.question.group || "Question"}</div>
          <h2 className="text-base font-semibold">{history.data.question.text}</h2>
          {!history.data.question.counts_in_scores && (
            <div className="mt-2 space-y-1 text-xs text-muted">
              <p className="flex flex-wrap items-center gap-2">
                <LeftOutBadge question={history.data.question} />
                {leftOutWhy}
              </p>
              <CountAnyway question={history.data.question} />
            </div>
          )}
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close" icon>
          <Icon name="x" />
        </Button>
      </div>
      {selSeries && selPoint && (
        <div className="flex shrink-0 flex-col gap-2 border-b border-line px-4 py-2.5 sm:flex-row sm:items-center">
          <select
            aria-label="AI app and model"
            className={cx(selectClass, "w-full min-w-0 text-xs sm:w-auto sm:flex-1")}
            value={lineKey(selSeries)}
            onChange={(e) => {
              const s = series.find((x) => lineKey(x) === e.target.value)!;
              pick((s.points.find((p) => p.run_id === selPoint.run_id) ?? s.points[s.points.length - 1]).answer_id);
            }}
          >
            {series.map((s) => (
              <option key={lineKey(s)} value={lineKey(s)}>
                {labels.line(s.pair)} ({fidelityInfo[s.fidelity].label})
              </option>
            ))}
          </select>
          <Segmented className="self-start sm:self-auto" label="View" value={raw ? "raw" : "analysis"} options={[{ id: "analysis", label: "Summary" }, { id: "raw", label: "Full answer" }]} onChange={(v) => view.set({ qv: v === "raw" ? "raw" : null })} />
        </div>
      )}
      <div className="min-h-0 flex-1 space-y-6 overflow-auto px-4 py-4">
        {!selSeries || !selPoint ? (
          <p className="text-sm text-muted">No answers to this question in this range.</p>
        ) : raw ? (
          <section>
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <h3 className="flex-1 text-sm font-semibold">
                {labels.line(selSeries.pair)}, check #{selPoint.run_id}
              </h3>
              <select aria-label="Check" className={cx(selectClass, "text-xs")} value={selPoint.answer_id} onChange={(e) => pick(Number(e.target.value))}>
                {[...selSeries.points].reverse().map((p) => (
                  <option key={p.answer_id} value={p.answer_id}>
                    Check #{p.run_id}, {shortDay(p.run_started_at)}
                  </option>
                ))}
              </select>
            </div>
            <AnswerView id={selPoint.answer_id} />
          </section>
        ) : (
          <>
            <Analysis series={selSeries} point={selPoint} label={labels.line(selSeries.pair)} onRaw={() => view.set({ qv: "raw" })} />

            <section>
              <h3 className="mb-2 text-sm font-semibold">Check history · {labels.line(selSeries.pair)}</h3>
              <ol className="divide-y divide-line rounded-surface border border-line bg-surface">
                {[...selSeries.points].reverse().map((p) => (
                  <li key={p.answer_id}>
                    <button
                      type="button"
                      onClick={() => pick(p.answer_id, { qv: "raw" })}
                      title="Read this check's full answer"
                      className={cx("flex w-full items-start gap-3 px-3 py-3.5 text-left hover:bg-subtle md:py-2", p.answer_id === selPoint.answer_id && "bg-subtle")}
                    >
                      <span className="w-24 shrink-0 text-xs text-muted">
                        #{p.run_id} · {shortDay(p.run_started_at)}
                      </span>
                      <span className="min-w-0 flex-1 text-xs">{p.summary || "No change"}</span>
                      <Mark point={p} />
                    </button>
                  </li>
                ))}
              </ol>
            </section>

            <section>
              <h3 className="mb-2 text-sm font-semibold">Your position per check</h3>
              <div className="overflow-hidden rounded-surface border border-line bg-surface">
                <ScrollX label="positions per check" startAtEnd>
                  <table className="w-full min-w-max text-sm">
                    <thead className="border-b border-line text-xs text-muted">
                      <tr>
                        <th scope="col" className="sticky left-0 z-[1] bg-surface px-3 py-2 text-left font-medium shadow-[inset_-1px_0_0_var(--line)]">
                          AI app and model
                        </th>
                        {runs.map(([id, at]) => (
                          <th key={id} scope="col" className="w-12 min-w-12 px-1 py-2 text-center font-medium" title={`Check #${id}, ${shortDay(at)}`}>
                            #{id}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-line">
                      {series.map((s) => (
                        <tr key={lineKey(s)}>
                          <th data-sticky scope="row" className="sticky left-0 z-[1] w-40 max-w-40 bg-surface px-3 py-1.5 text-left text-xs leading-tight font-medium shadow-[inset_-1px_0_0_var(--line)] sm:w-56 sm:max-w-56">
                            <span className="block">{labels.surfaceTitle(s.pair.surface)}</span>
                            {(s.pair.model || s.fidelity === "approximation") && (
                              <span className="block font-normal text-muted [overflow-wrap:anywhere]">
                                {s.pair.model}
                                {s.fidelity === "approximation" && <span className="text-warn">{s.pair.model && " · "}stand-in</span>}
                              </span>
                            )}
                          </th>
                          {runs.map(([id]) => {
                            const p = s.points.find((x) => x.run_id === id);
                            return (
                              <td key={id} className="px-0.5 py-1">
                                <Mark point={p} selected={p?.answer_id === selPoint.answer_id} onClick={p && (() => pick(p.answer_id))} />
                              </td>
                            );
                          })}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </ScrollX>
              </div>
              <p className="mt-1.5 text-xs text-faint">
                <span className="font-semibold text-success">#2</span> named you, as the 2nd brand. <span className="text-info">Linked</span> linked to your site without naming you. — did not name you. · not asked. Pick a mark to
                see that answer.
              </p>
            </section>

            <section>
              <h3 className="mb-2 text-sm font-semibold">Each AI app</h3>
              <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {series.map((s) => (
                  <SeriesSummary key={lineKey(s)} s={s} label={labels.line(s.pair)} />
                ))}
              </ul>
            </section>
          </>
        )}
      </div>
    </div>
  );
}

/** The selected AI app's answer in one check: where you stood, who was named instead, and what it linked to. */
function Analysis({ series: s, point, label, onRaw }: { series: HistorySeries; point: HistoryPoint; label: string; onRaw: () => void }) {
  const answer = useQuery({ queryKey: keys.answer(point.answer_id), queryFn: () => api.answer(point.answer_id), placeholderData: (prev) => prev });
  const competitors = answer.data?.mentions.filter((m) => m.kind === "competitor") ?? [];
  return (
    <section className="space-y-4">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
        <div className="rounded-surface border border-line bg-surface px-3 py-2">
          <div className="text-xs text-muted">Named you</div>
          <div className="text-lg font-semibold tabular-nums">
            {s.named} <span className="text-sm font-normal text-muted">of {s.answered} checks</span>
          </div>
        </div>
        <div className="rounded-surface border border-line bg-surface px-3 py-2">
          <div className="text-xs text-muted">Position in check #{point.run_id}</div>
          <div className="text-lg font-semibold tabular-nums">{point.position ? `#${point.position}` : point.status === "ok" && point.cited ? "Linked" : "–"}</div>
        </div>
        <div className="col-span-2 rounded-surface border border-line bg-surface px-3 py-2 sm:col-span-1">
          <div className="text-xs text-muted">Best position</div>
          <div className="text-lg font-semibold tabular-nums">{s.best_position ? `#${s.best_position}` : "–"}</div>
        </div>
      </div>
      <p className="flex flex-wrap items-center gap-2 text-xs text-muted">
        <Badge tone={s.fidelity === "approximation" ? "warn" : "outline"}>{fidelityInfo[s.fidelity].label}</Badge>
        {label}, check #{point.run_id}, {shortDay(point.run_started_at)}.
        <button type="button" onClick={onRaw} className="inline-flex h-11 items-center font-medium text-fg hover:underline md:h-auto">
          Read the answer
        </button>
      </p>
      {answer.isPending ? (
        <Pending />
      ) : answer.isError ? (
        <ErrorNote error={answer.error} />
      ) : answer.data.status !== "ok" ? (
        <p className="text-sm text-muted">{answer.data.status === "failed" ? `The call failed: ${answer.data.error || "no reason given"}.` : "This AI app showed no AI answer for this question."}</p>
      ) : (
        <>
          <div>
            <h3 className="mb-2 text-sm font-semibold">{answer.data.mentioned ? "Brands named alongside you" : "Brands mentioned instead"}</h3>
            {competitors.length === 0 ? (
              <p className="text-sm text-muted">No competitor was named.</p>
            ) : (
              <ul className="flex flex-wrap gap-1.5">
                {competitors.map((m) => (
                  <li key={m.brand_id}>
                    <Badge tone="outline">
                      {m.position && <span className="text-faint tabular-nums">#{m.position}</span>}
                      {m.name}
                    </Badge>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div>
            <h3 className="mb-2 text-sm font-semibold">Pages it linked to</h3>
            {answer.data.citations.length === 0 ? (
              <p className="text-sm text-muted">This answer linked to no pages.</p>
            ) : (
              <ol className="divide-y divide-line rounded-surface border border-line bg-surface text-xs">
                {answer.data.citations.map((c, i) => (
                  <li key={i} className="flex flex-col gap-1 px-3 py-2 sm:flex-row sm:items-center sm:gap-3">
                    <span className="flex min-w-0 flex-1 items-center gap-2">
                      <span className="text-faint tabular-nums">{i + 1}</span>
                      <a href={c.url} target="_blank" rel="noreferrer noopener" className={cx("min-w-0 truncate py-3.5 hover:underline md:py-0", c.is_self && "font-semibold")} title={c.url}>
                        {c.url.replace(/^https?:\/\/(www\.)?/, "")}
                      </a>
                      {c.is_self && <Badge tone="info">Yours</Badge>}
                    </span>
                    <span className="flex shrink-0 flex-wrap items-center gap-1 pl-5 sm:pl-0">
                      <PageMark check={c.page} />
                      {c.page?.competitors.map((b) => (
                        <Badge key={b} tone="outline">
                          {b}
                        </Badge>
                      ))}
                    </span>
                  </li>
                ))}
              </ol>
            )}
          </div>
        </>
      )}
    </section>
  );
}

function SeriesSummary({ s, label }: { s: HistorySeries; label: string }) {
  const latest = s.points[s.points.length - 1];
  return (
    <li className="min-w-0 rounded-surface border border-line bg-surface px-3 py-2">
      <div className="flex flex-wrap items-start justify-between gap-x-2 gap-y-1">
        <span className="min-w-0 text-sm font-medium [overflow-wrap:anywhere]">{label}</span>
        <Badge tone={s.fidelity === "approximation" ? "warn" : "outline"}>{fidelityInfo[s.fidelity].label}</Badge>
      </div>
      <p className="mt-1 text-xs text-muted">
        Named you in {s.named} of {s.answered} {s.answered === 1 ? "check" : "checks"}
        {s.best_position ? `, best position #${s.best_position}` : ""}. Linked to your site in {s.cited}.
      </p>
      <p className="text-xs text-faint">Latest: {latest.summary || (latest.mentioned ? `named at #${latest.position}` : "not named")}</p>
    </li>
  );
}

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

function AnswerView({ id }: { id: number }) {
  const answer = useQuery({ queryKey: keys.answer(id), queryFn: () => api.answer(id), placeholderData: (prev) => prev });
  const brands = useBrands();
  const settings = useSettings();
  if (answer.isPending) return <Pending />;
  if (answer.isError) return <ErrorNote error={answer.error} />;
  const a: Answer = answer.data;
  const names = new Map<string, "self" | "competitor">();
  const add = (b: Pick<BrandEntry, "name" | "aliases">, kind: "self" | "competitor") => [b.name, ...(b.aliases ?? [])].filter(Boolean).forEach((n) => names.set(n, kind));
  for (const b of brands.data ?? []) if (b.kind === "competitor") add(b, "competitor");
  for (const b of brands.data ?? []) if (b.kind === "self") add(b, "self");
  if (settings.data) add(settings.data.brand, "self");
  const pattern = [...names.keys()].sort((x, y) => y.length - x.length).map(escape).join("|");
  const brandRe = pattern ? new RegExp(`\\b(${pattern})\\b`, "g") : null;
  const mark = (text: string) =>
    brandRe
      ? text.split(brandRe).map((p, i) =>
          i % 2 === 1 ? (
            <mark key={i} className={cx("rounded-inner px-0.5", names.get(p) === "self" ? "bg-you font-semibold text-you-fg" : "bg-subtle-strong text-fg")}>
              {p}
            </mark>
          ) : (
            p
          ),
        )
      : text;

  return (
    <div className="rounded-surface border border-line bg-surface">
      <div className="flex flex-wrap items-center gap-2 border-b border-line px-3 py-2 text-xs text-muted">
        {a.mentioned ? <Badge tone="success">Named you at #{a.position}</Badge> : <Badge>Not named</Badge>}
        {a.cited && <Badge tone="info">Linked to your site</Badge>}
        {a.reported_model && <span>Model {a.reported_model}</span>}
        <span>{a.cost_usd == null ? <span className="text-warn">No price reported</span> : money(a.cost_usd)}</span>
        <span className="ml-auto flex items-center gap-2">
          <mark className="rounded-inner bg-you px-1 font-semibold text-you-fg">You</mark>
          <mark className="rounded-inner bg-subtle-strong px-1 text-fg">Competitor</mark>
        </span>
      </div>
      {a.status === "failed" ? (
        <p className="px-3 py-3 text-sm text-danger">The call failed: {a.error || "no reason given"}.</p>
      ) : a.status === "no_answer" ? (
        <p className="px-3 py-3 text-sm text-muted">This AI app showed no AI answer for this question.</p>
      ) : (
        <div className="max-h-[28rem] overflow-auto px-3 py-3 text-sm leading-relaxed [overflow-wrap:anywhere]">
          <Markdown text={a.text} mark={mark} />
        </div>
      )}
      {a.citations.length > 0 && (
        <div className="border-t border-line px-3 py-2">
          <h4 className="mb-1 text-xs font-medium text-muted">Links</h4>
          <ol className="space-y-0.5 text-xs">
            {a.citations.map((c, i) => (
              <li key={i} className="flex items-center gap-2">
                <span className="text-faint tabular-nums">{i + 1}</span>
                <a href={c.url} target="_blank" rel="noreferrer noopener" className={cx("min-w-0 truncate py-3.5 hover:underline md:py-0", c.is_self && "font-semibold")}>
                  {c.url}
                </a>
                {c.is_self && <Badge tone="info">Yours</Badge>}
              </li>
            ))}
          </ol>
        </div>
      )}
      {a.fan_out && a.fan_out.length > 0 && (
        <div className="border-t border-line px-3 py-2 text-xs">
          <span className="text-muted">The AI searched Google for </span>
          {a.fan_out.map((f, i) => (
            <span key={i}>
              {i > 0 && ", "}“{f}”
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
