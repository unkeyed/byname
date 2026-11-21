import { useQuery } from "@tanstack/react-query";
import { api, type AnswerSummary, type Dot, type DotState, type PromptRow, type ResultsFilter, type Run, type RunRef } from "../../api";
import { fidelityInfo, shortDay, useLabels } from "../../lines";
import { keys, useAnsweredRuns, useSettings, useSurfaces } from "../../queries";
import { Badge, Card, cx, EmptyState, ErrorNote, inputClass, PageSpinner, ScrollX, selectClass, useWide } from "../../ui";
import { runTime, useFilter, useView } from "../../view";
import { ViewHeader } from "./Header";
import { CellMark } from "./QuestionsTable";
import { Tabs, useTab } from "./Tabs";

const tabs = [
  { id: "prompts", label: "Prompts" },
  { id: "answers", label: "Answers" },
] as const;

export function Prompts() {
  const settings = useSettings();
  const runs = useAnsweredRuns();
  const { filter, ready } = useFilter();
  const tab = useTab(tabs);
  const brand = settings.data?.brand.name || "you";

  let body;
  if (runs.isPending || !ready) body = <PageSpinner />;
  else if (runs.isError) body = <ErrorNote error={runs.error} />;
  else if (!runs.data?.length)
    body = (
      <EmptyState icon="message" title="No checks yet">
        Once a check has run, every question shows here with the surfaces and models that named {brand}.
      </EmptyState>
    );
  else if (tab === "prompts") body = <PromptList filter={filter} />;
  else body = <AnswerList run={runs.data[0]} filter={filter} />;

  return (
    <>
      <ViewHeader
        title="Prompts"
        subtitle={tab === "prompts" ? `Every question, which surfaces and models name ${brand}, and who they name instead.` : `Every answer from the latest check, with ${brand}'s position and how many sources it cited.`}
        range={tab === "prompts"}
      />
      <div className="relative min-h-0 flex-1 overflow-auto px-3 py-4 md:px-5 md:py-5">
        <div className="mx-auto max-w-6xl space-y-4">
          <Tabs label="Prompts views" tabs={tabs} current={tab} />
          {body}
        </div>
      </div>
    </>
  );
}

const dotLook: Record<DotState, { label: string; className: string }> = {
  named: { label: "Named you", className: "bg-success" },
  cited: { label: "Cited you, not named", className: "bg-info" },
  not_named: { label: "Not named", className: "border border-line-hover bg-transparent" },
  failed: { label: "Every call failed", className: "bg-subtle-strong" },
};

function RunDot({ dot, run }: { dot: Dot | null; run: RunRef }) {
  const when = `Check #${run.id}, ${shortDay(run.started_at)}`;
  if (!dot) return <span className="size-2.5 shrink-0" title={`${when}: not asked`} />;
  const look = dotLook[dot.state];
  const detail = dot.state === "failed" ? look.label : `${look.label}: named on ${dot.named} of ${dot.answered}`;
  return <span role="img" aria-label={`${when}: ${detail}`} title={`${when}: ${detail}`} className={cx("size-2.5 shrink-0 rounded-full", look.className)} />;
}

/** At most this many dots, the newest; the drawer has the full history. */
const maxDots = 30;

function RunDots({ row, runs }: { row: PromptRow; runs: RunRef[] }) {
  const from = Math.max(0, runs.length - maxDots);
  return (
    <span className="flex flex-wrap items-center gap-1">
      {runs.slice(from).map((r, i) => (
        <RunDot key={r.id} run={r} dot={row.dots[from + i]} />
      ))}
    </span>
  );
}

function DotKey() {
  return (
    <p className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3 text-xs text-faint">
      {(Object.keys(dotLook) as DotState[]).map((k) => (
        <span key={k} className="inline-flex items-center gap-1.5">
          <span className={cx("size-2.5 rounded-full", dotLook[k].className)} aria-hidden="true" />
          {dotLook[k].label}
        </span>
      ))}
      <span>One dot per check, oldest first, counting the surfaces and models in view.</span>
    </p>
  );
}

export function Instead({ names, max = 3 }: { names: string[]; max?: number }) {
  if (names.length === 0) return <span className="text-xs text-faint">–</span>;
  return (
    <span className="flex flex-wrap gap-1" title={names.join(", ")}>
      {names.slice(0, max).map((n) => (
        <Badge key={n} tone="outline">
          {n}
        </Badge>
      ))}
      {names.length > max && <span className="text-xs text-muted">+{names.length - max}</span>}
    </span>
  );
}

function PromptList({ filter }: { filter: ResultsFilter }) {
  const view = useView();
  const wide = useWide();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const prompts = useQuery({ queryKey: keys.prompts(filter), queryFn: () => api.prompts(filter), placeholderData: (p) => p });
  if (prompts.isPending) return <PageSpinner />;
  if (prompts.isError) return <ErrorNote error={prompts.error} />;
  const { rows, runs, latest_run_id } = prompts.data;
  if (rows.length === 0) return <Card className="px-4 py-10 text-center text-sm text-muted">No answers in this range. Widen the date range to see the questions.</Card>;

  const search = view.get("search").toLowerCase();
  const group = view.get("group");
  const groups = [...new Set(rows.map((r) => r.question.group))];
  const shown = rows.filter((r) => (!search || r.question.text.toLowerCase().includes(search)) && (!group || r.question.group === group));
  const open = (r: PromptRow) => view.set({ q: r.question.id, qa: null, qv: null }, { push: true });
  const latest = runs[runs.length - 1];
  const namedBy = (r: PromptRow) => r.named_by.map((s) => `${labels.line(s.pair)}${s.fidelity === "approximation" ? " (approx.)" : ""}`);

  return (
    <Card className={cx(prompts.isPlaceholderData && "opacity-60")}>
      <div className="flex flex-col gap-2 border-b border-line px-4 py-3 sm:flex-row sm:flex-wrap sm:items-center">
        <input type="search" aria-label="Search questions" placeholder="Search questions" value={view.get("search")} onChange={(e) => view.set({ search: e.target.value })} className={cx(inputClass, "sm:max-w-64")} />
        <select aria-label="Group" className={cx(selectClass, "text-xs")} value={group} onChange={(e) => view.set({ group: e.target.value })}>
          <option value="">All groups</option>
          {groups.map((g) => (
            <option key={g} value={g}>
              {g}
            </option>
          ))}
        </select>
        <p className="text-xs text-faint sm:ml-auto">
          {rows.length} {rows.length === 1 ? "question" : "questions"} as check #{latest_run_id} asked them, {shortDay(latest.started_at)}
          {runs.length > maxDots && `. Dots show the newest ${maxDots} of ${runs.length} checks`}
        </p>
      </div>
      {wide ? (
        <ScrollX label="prompts table">
          <table className="w-full min-w-max text-sm">
            <thead className="border-b border-line text-xs text-muted">
              <tr>
                <th scope="col" className="sticky left-0 z-[1] w-72 min-w-72 bg-surface px-3 py-2 text-left font-medium shadow-[inset_-1px_0_0_var(--line)]">
                  Question
                </th>
                <th scope="col" className="px-3 py-2 text-left font-medium">Named by</th>
                <th scope="col" className="px-3 py-2 text-left font-medium">Best</th>
                <th scope="col" className="px-3 py-2 text-left font-medium">Named instead</th>
                <th scope="col" className="px-3 py-2 text-left font-medium">Checks</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {shown.map((r) => (
                <tr key={r.question.id} className="group cursor-pointer hover:bg-subtle" onClick={() => open(r)}>
                  <td data-sticky className="sticky left-0 z-[1] w-72 min-w-72 max-w-72 bg-surface px-3 py-2 shadow-[inset_-1px_0_0_var(--line)] group-hover:bg-[color-mix(in_oklab,var(--surface),var(--fg)_5%)]">
                    <button type="button" className="text-left font-medium hover:underline" onClick={(e) => (e.stopPropagation(), open(r))}>
                      {r.question.text}
                    </button>
                    <div className="text-xs text-faint">{r.question.group}</div>
                  </td>
                  <td className="max-w-64 px-3 py-2 align-top">
                    <div className="tabular-nums">
                      {r.named_by.length} <span className="text-faint">of {r.answered}</span>
                    </div>
                    {r.named_by.length > 0 && <div className="truncate text-xs text-muted" title={namedBy(r).join(", ")}>{namedBy(r).join(", ")}</div>}
                  </td>
                  <td className="px-3 py-2 align-top tabular-nums">{r.best_position ? `#${r.best_position}` : <span className="text-faint">–</span>}</td>
                  <td className="max-w-56 px-3 py-2 align-top">
                    <Instead names={r.instead} />
                  </td>
                  <td className="px-3 py-2 align-top">
                    <RunDots row={r} runs={runs} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollX>
      ) : (
        <ul className="divide-y divide-line">
          {shown.map((r) => (
            <li key={r.question.id}>
              <button type="button" onClick={() => open(r)} className="block w-full space-y-2 px-4 py-3 text-left hover:bg-subtle">
                <span className="block">
                  <span className="block font-medium">{r.question.text}</span>
                  <span className="block text-xs text-faint">{r.question.group}</span>
                </span>
                <span className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted tabular-nums">
                  <span>
                    Named by <span className="font-medium text-fg">{r.named_by.length}</span> of {r.answered}
                  </span>
                  {r.best_position && (
                    <span>
                      Best <span className="font-medium text-fg">#{r.best_position}</span>
                    </span>
                  )}
                </span>
                {r.instead.length > 0 && (
                  <span className="flex items-start gap-2 text-xs text-muted">
                    <span className="shrink-0 pt-0.5">Instead</span>
                    <Instead names={r.instead} />
                  </span>
                )}
                <RunDots row={r} runs={runs} />
              </button>
            </li>
          ))}
        </ul>
      )}
      {shown.length === 0 && <p className="px-4 py-8 text-center text-sm text-muted">No question matches these filters.</p>}
      <div className="border-t border-line">
        <DotKey />
      </div>
    </Card>
  );
}

function AnswerList({ run, filter }: { run: Run; filter: ResultsFilter }) {
  const view = useView();
  const wide = useWide();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const answers = useQuery({ queryKey: keys.answers(run.id), queryFn: () => api.runs.answers(run.id) });
  if (answers.isPending) return <PageSpinner />;
  if (answers.isError) return <ErrorNote error={answers.error} />;
  const search = view.get("search").toLowerCase();
  const shown = answers.data.filter(
    (a) => (!filter.surface || a.pair.surface === filter.surface) && (!filter.model || a.pair.model === filter.model) && (!search || a.question.text.toLowerCase().includes(search)),
  );
  const open = (a: AnswerSummary) => view.set({ q: a.question.id, qa: a.id, qv: "raw" }, { push: true });
  const competitors = (a: AnswerSummary) => a.mentions.filter((m) => m.kind === "competitor").length;
  const mark = (a: AnswerSummary) => <CellMark cell={{ answer_id: a.id, status: a.status, mentioned: a.mentioned, cited: a.cited, position: a.position, outcome: "not_comparable" }} />;
  const pairLabel = (a: AnswerSummary) => (
    <>
      {labels.line(a.pair)}
      {a.fidelity === "approximation" && <span className="text-warn"> · approx.</span>}
    </>
  );

  return (
    <Card>
      <div className="flex flex-col gap-2 border-b border-line px-4 py-3 sm:flex-row sm:items-center">
        <input type="search" aria-label="Search answers by question" placeholder="Search by question" value={view.get("search")} onChange={(e) => view.set({ search: e.target.value })} className={cx(inputClass, "sm:max-w-64")} />
        <p className="text-xs text-faint sm:ml-auto">
          {shown.length} {shown.length === 1 ? "answer" : "answers"} from check #{run.id}, {shortDay(runTime(run))}
        </p>
      </div>
      {wide ? (
        <ScrollX label="answers table">
          <table className="w-full min-w-max text-sm">
            <thead className="border-b border-line text-xs text-muted">
              <tr>
                <th scope="col" className="w-80 px-3 py-2 text-left font-medium">Question</th>
                <th scope="col" className="px-3 py-2 text-left font-medium">Surface and model</th>
                <th scope="col" className="px-3 py-2 text-left font-medium">You</th>
                <th scope="col" className="px-3 py-2 text-right font-medium" title="Competitors the answer named">Competitors</th>
                <th scope="col" className="px-3 py-2 text-right font-medium" title="Pages the answer cited">Sources</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {shown.map((a) => (
                <tr key={a.id} className="cursor-pointer hover:bg-subtle" onClick={() => open(a)}>
                  <td className="max-w-80 px-3 py-2">
                    <button type="button" className="text-left font-medium hover:underline" onClick={(e) => (e.stopPropagation(), open(a))}>
                      {a.question.text}
                    </button>
                  </td>
                  <td className="px-3 py-2 text-xs" title={fidelityInfo[a.fidelity].detail}>
                    {pairLabel(a)}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap">{mark(a)}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{competitors(a)}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{a.citation_count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollX>
      ) : (
        <ul className="divide-y divide-line">
          {shown.map((a) => (
            <li key={a.id}>
              <button type="button" onClick={() => open(a)} className="block w-full space-y-1.5 px-4 py-3 text-left hover:bg-subtle">
                <span className="block font-medium">{a.question.text}</span>
                <span className="block text-xs text-muted">{pairLabel(a)}</span>
                <span className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted tabular-nums">
                  {mark(a)}
                  <span>
                    <span className="font-medium text-fg">{competitors(a)}</span> competitors
                  </span>
                  <span>
                    <span className="font-medium text-fg">{a.citation_count}</span> sources
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {shown.length === 0 && <p className="px-4 py-8 text-center text-sm text-muted">No answer matches these filters.</p>}
    </Card>
  );
}
