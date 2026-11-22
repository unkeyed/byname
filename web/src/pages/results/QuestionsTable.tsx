import { useQuery } from "@tanstack/react-query";
import { api, type Cell, type Outcome, type QuestionRow, type ResultsFilter, type Run, type Series } from "../../api";
import { fidelityInfo, shortDay, useLabels } from "../../lines";
import { keys, useSurfaces } from "../../queries";
import { cx, ErrorNote, Icon, inputClass, Pending, ScrollX, selectClass, useWide } from "../../ui";
import { runTime, useView } from "../../view";

/** Status marks always pair an icon with words, never colour alone. */
export const outcomeMark: Record<Outcome, { icon: "up" | "down" | "updown" | null; label: string; className: string }> = {
  gained: { icon: "up", label: "Gained", className: "text-success" },
  lost: { icon: "down", label: "Lost", className: "text-danger" },
  moved: { icon: "updown", label: "Moved", className: "text-info" },
  unchanged: { icon: null, label: "Unchanged", className: "text-faint" },
  not_comparable: { icon: null, label: "Nothing to compare", className: "text-faint" },
};

export function CellMark({ cell }: { cell: Cell | null }) {
  if (!cell) return <span className="text-faint" title="Not asked on this pair">–</span>;
  const o = outcomeMark[cell.outcome];
  let body;
  if (cell.status === "failed") body = <span className="text-xs text-faint" title="The call failed, so it counts neither way">Failed</span>;
  else if (cell.status === "no_answer") body = <span className="text-xs text-faint" title="The surface showed no AI answer">No answer</span>;
  else if (cell.mentioned)
    body = (
      <span className="inline-flex h-5 items-center gap-1 rounded-control bg-success-soft px-1.5 text-xs font-semibold text-success tabular-nums" title={`Named you${cell.position ? ` at #${cell.position}` : ""}${cell.cited ? " and cited you" : ""}`}>
        #{cell.position ?? "?"}
        {cell.cited && <Icon name="link" className="size-3" />}
      </span>
    );
  else if (cell.cited)
    body = (
      <span className="inline-flex h-5 items-center gap-1 rounded-control bg-info-soft px-1.5 text-xs font-medium text-info" title="Cited your domain without naming you">
        <Icon name="link" className="size-3" />
        Cited
      </span>
    );
  else body = <span className="text-xs text-faint" title="Did not name you">Not named</span>;
  return (
    <span className="inline-flex items-center gap-1">
      {body}
      {o.icon && (
        <span className={o.className} title={`${o.label} since the previous check`}>
          <Icon name={o.icon} className="size-3" />
          <span className="sr-only">{o.label}</span>
        </span>
      )}
    </span>
  );
}

export function ChangeMark({ row }: { row: Pick<QuestionRow, "gained" | "lost" | "moved"> }) {
  const parts = (["gained", "lost", "moved"] as const).filter((k) => row[k] > 0);
  if (parts.length === 0) return <span className="text-xs text-faint">No change</span>;
  return (
    <span className="inline-flex items-center gap-2 text-xs font-medium tabular-nums">
      {parts.map((k) => (
        <span key={k} className={cx("inline-flex items-center gap-0.5", outcomeMark[k].className)} title={`${row[k]} ${outcomeMark[k].label.toLowerCase()}`}>
          <Icon name={outcomeMark[k].icon!} className="size-3" />
          {row[k]}
          <span className="sr-only">{outcomeMark[k].label.toLowerCase()}</span>
        </span>
      ))}
    </span>
  );
}

const results = [
  { id: "", label: "Any result" },
  { id: "named", label: "Named you somewhere" },
  { id: "missed", label: "Missed on some pair" },
  { id: "never", label: "Never named you" },
  { id: "cited", label: "Cited you" },
  { id: "changed", label: "Changed since last check" },
  { id: "gained", label: "Gained" },
  { id: "lost", label: "Lost" },
] as const;

const keep: Record<string, (r: QuestionRow) => boolean> = {
  named: (r) => r.named > 0,
  missed: (r) => r.named < r.answered,
  never: (r) => r.named === 0,
  cited: (r) => r.cited > 0,
  changed: (r) => r.gained + r.lost + r.moved > 0,
  gained: (r) => r.gained > 0,
  lost: (r) => r.lost > 0,
};

/** Lower sorts first: named by position, then cited, then not named, then nothing to read. */
function cellRank(c: Cell | null): number {
  if (!c || c.status === "failed") return 10_000;
  if (c.mentioned) return c.position ?? 999;
  if (c.cited) return 1_000;
  return c.status === "no_answer" ? 3_000 : 2_000;
}

const sorters: Record<string, (a: QuestionRow, b: QuestionRow) => number> = {
  question: (a, b) => a.question.text.localeCompare(b.question.text),
  named: (a, b) => a.named - b.named || a.answered - b.answered,
  best: (a, b) => (a.best_position ?? 999) - (b.best_position ?? 999),
  change: (a, b) => a.gained - a.lost - (b.gained - b.lost) || a.moved - b.moved,
};

export function QuestionsTable({ run, filter }: { run: Run; filter: ResultsFilter }) {
  const view = useView();
  const wide = useWide();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const table = useQuery({ queryKey: keys.questions(run.id, filter), queryFn: () => api.runs.questions(run.id, filter) });
  if (table.isPending) return <Pending />;
  if (table.isError) return <ErrorNote error={table.error} />;
  const { columns, rows, previous_run_id } = table.data;

  const search = view.get("search").toLowerCase();
  const group = view.get("group");
  const result = view.get("result");
  const sort = view.get("sort");
  const desc = view.get("dir") === "desc";
  const groups = [...new Set(rows.map((r) => r.question.group))];
  let shown = rows.filter((r) => (!search || r.question.text.toLowerCase().includes(search)) && (!group || r.question.group === group) && (!keep[result] || keep[result](r)));
  const sorter = sort.startsWith("c") ? (a: QuestionRow, b: QuestionRow) => cellRank(a.cells[+sort.slice(1)]) - cellRank(b.cells[+sort.slice(1)]) : sorters[sort];
  if (sorter) {
    shown = [...shown].sort(sorter);
    if (desc) shown.reverse();
  }
  const setSort = (k: string) => view.set(sort === k ? { dir: desc ? null : "desc" } : { sort: k, dir: null });
  const SortHead = ({ k, children, className }: { k: string; children: React.ReactNode; className?: string }) => (
    <th scope="col" aria-sort={sort === k ? (desc ? "descending" : "ascending") : undefined} className={cx("px-2.5 py-2 text-left font-medium", className)}>
      <button type="button" onClick={() => setSort(k)} className="inline-flex items-center gap-1 text-left hover:text-fg">
        {children}
        {sort === k && <Icon name={desc ? "down" : "up"} className="size-3" />}
      </button>
    </th>
  );

  const open = (r: QuestionRow) => view.set({ q: r.question.id, qa: null }, { push: true });
  const surfaceTitle = labels.surfaceTitle;

  return (
    <div>
      <div className="flex flex-col gap-2 border-y border-line px-4 py-3 sm:flex-row sm:flex-wrap sm:items-center">
        <input type="search" aria-label="Search questions" placeholder="Search questions" value={view.get("search")} onChange={(e) => view.set({ search: e.target.value })} className={cx(inputClass, "sm:max-w-64")} />
        <div className="grid grid-cols-2 gap-2 sm:flex">
          <select aria-label="Group" className={cx(selectClass, "text-xs")} value={group} onChange={(e) => view.set({ group: e.target.value })}>
            <option value="">All groups</option>
            {groups.map((g) => (
              <option key={g} value={g}>
                {g}
              </option>
            ))}
          </select>
          <select aria-label="Result" className={cx(selectClass, "text-xs")} value={result} onChange={(e) => view.set({ result: e.target.value })}>
            {results.map((r) => (
              <option key={r.id} value={r.id}>
                {r.label}
              </option>
            ))}
          </select>
          {!wide && (
            <select aria-label="Sort" className={cx(selectClass, "col-span-2 text-xs")} value={sort in sorters ? `${sort}:${desc ? "desc" : ""}` : ""} onChange={(e) => {
              const [k, d] = e.target.value.split(":");
              view.set({ sort: k || null, dir: d || null });
            }}>
              <option value="">In question order</option>
              <option value="best:">Best position first</option>
              <option value="named:desc">Named you most first</option>
              <option value="named:">Named you least first</option>
              <option value="change:">Biggest losses first</option>
              <option value="change:desc">Biggest gains first</option>
              <option value="question:">By question, A to Z</option>
            </select>
          )}
        </div>
        <p className="text-xs text-faint sm:ml-auto">
          Check #{run.id}, {shortDay(runTime(run))}
          {previous_run_id ? `, changes since #${previous_run_id}` : ", the first check"}
        </p>
      </div>
      {wide ? (
        <ScrollX label="questions table">
          <table className="w-full min-w-max text-sm">
            <thead className="border-b border-line text-xs text-muted">
              <tr>
                <SortHead k="question" className="sticky left-0 z-[1] w-64 align-bottom min-w-64 bg-surface shadow-[inset_-1px_0_0_var(--line)]">
                  Question
                </SortHead>
                {columns.map((c, i) => (
                  <SortHead key={i} k={`c${i}`} className="max-w-32 align-bottom">
                    <ColumnLabel column={c} surfaceTitle={surfaceTitle} />
                  </SortHead>
                ))}
                <SortHead k="named" className="align-bottom">Named you</SortHead>
                <SortHead k="best" className="align-bottom">Best</SortHead>
                <SortHead k="change" className="align-bottom">Change</SortHead>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {shown.map((r) => (
                <tr key={r.question.id} className="group cursor-pointer hover:bg-subtle" onClick={() => open(r)}>
                  <td data-sticky className="sticky left-0 z-[1] w-64 min-w-64 bg-surface px-3 py-2 shadow-[inset_-1px_0_0_var(--line)] group-hover:bg-[color-mix(in_oklab,var(--surface),var(--fg)_5%)]">
                    <button type="button" className="text-left font-medium hover:underline" onClick={(e) => (e.stopPropagation(), open(r))}>
                      {r.question.text}
                    </button>
                    <div className="text-xs text-faint">{r.question.group}</div>
                  </td>
                  {r.cells.map((c, i) => (
                    <td key={i} className="px-2.5 py-2 whitespace-nowrap">
                      <CellMark cell={c} />
                    </td>
                  ))}
                  <td className="px-3 py-2 whitespace-nowrap tabular-nums">
                    {r.named} <span className="text-faint">of {r.answered}</span>
                  </td>
                  <td className="px-3 py-2 tabular-nums">{r.best_position ? `#${r.best_position}` : <span className="text-faint">–</span>}</td>
                  <td className="px-3 py-2 whitespace-nowrap">
                    <ChangeMark row={r} />
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
              <button type="button" onClick={() => open(r)} className="block w-full px-4 py-3 text-left hover:bg-subtle">
                <span className="block font-medium">{r.question.text}</span>
                <span className="block text-xs text-faint">{r.question.group}</span>
                <span className="mt-2 grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-1 text-xs">
                  {r.cells.map((c, i) => (
                    <span key={i} className="contents">
                      <span className="truncate text-muted">
                        {surfaceTitle(columns[i].pair.surface)}
                        {columns[i].pair.model && <span className="text-faint"> · {columns[i].pair.model}</span>}
                      </span>
                      <span className="text-right">
                        <CellMark cell={c} />
                      </span>
                    </span>
                  ))}
                </span>
                <span className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-line pt-2 text-xs text-muted">
                  <span className="tabular-nums">
                    Named you <span className="font-medium text-fg">{r.named}</span> of {r.answered}
                  </span>
                  {r.best_position && <span className="tabular-nums">Best <span className="font-medium text-fg">#{r.best_position}</span></span>}
                  <span className="ml-auto">
                    <ChangeMark row={r} />
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {shown.length === 0 && <p className="px-4 py-8 text-center text-sm text-muted">No question matches these filters.</p>}
    </div>
  );
}

/** A column's surface, then its model (or fidelity) on a line of its own, so the header stays narrow. */
function ColumnLabel({ column: c, surfaceTitle }: { column: Series; surfaceTitle: (key: string) => string }) {
  return (
    <span className="block leading-tight">
      <span className="block text-fg">{surfaceTitle(c.pair.surface)}</span>
      <span className="block font-normal [overflow-wrap:anywhere]">
        {c.pair.model || fidelityInfo[c.fidelity].label}
        {c.fidelity === "approximation" && <span className="text-warn"> · approx.</span>}
      </span>
    </span>
  );
}
