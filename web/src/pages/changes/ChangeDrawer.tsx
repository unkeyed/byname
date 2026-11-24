import type { ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Change, type ChangeOutcome, type ChangeRead, type ChangeStep, type ChangeWindow, type UrlInspection } from "../../api";
import { keys } from "../../queries";
import { Badge, Button, Card, ConfirmButton, cx, ErrorNote, Icon, LoadBoundary, Pending, scrollArea, ScrollX, useModal } from "../../ui";
import { useView } from "../../view";
import { count, ctr, dayRange, figureInfo, fullDay, position } from "../search/model";
import { FigureLegend } from "../search/Search";

/** Site changes read Google alone, so their figures are Google's. */
const google = figureInfo("Google");
import { changeStatus, deltaMetrics, kindInfo, outcomeInfo, stepInfo, useZone, type Zone } from "./model";

/** Opens the drawer on a change, as a link that Back closes. */
export function useOpenChange() {
  const view = useView();
  return (id: number) => view.set({ chg: id }, { push: true });
}

/** One change with its reads, timeline and inspections: open wherever ?chg= names one. */
export function ChangeDrawer({ changes, canEdit }: { changes: Change[] | undefined; canEdit: boolean }) {
  const view = useView();
  const id = Number(view.get("chg")) || 0;
  const ref = useModal(id > 0);
  const close = () => view.set({ chg: null });
  return (
    <dialog
      ref={ref}
      onClose={close}
      onClick={(e) => e.target === ref.current && close()}
      aria-label="Change"
      data-drawer
      className="m-0 ml-auto h-dvh max-h-none w-full max-w-3xl border-0 border-l border-line bg-bg p-0 text-fg shadow-floating"
    >
      {id > 0 && (
        <LoadBoundary key={id} className="h-full">
          <Body id={id} listed={changes?.find((c) => c.id === id)} canEdit={canEdit} onClose={close} />
        </LoadBoundary>
      )}
    </dialog>
  );
}

function Body({ id, listed, canEdit, onClose }: { id: number; listed: Change | undefined; canEdit: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const zone = useZone();
  const detail = useQuery({ queryKey: keys.change(id), queryFn: () => api.changes.get(id) });
  const remove = useMutation({
    mutationFn: () => api.changes.remove(id),
    onSuccess: async () => {
      onClose();
      await qc.invalidateQueries({ queryKey: keys.changes });
    },
  });
  const c = detail.data ?? listed;

  if (!c) {
    return (
      <div className="space-y-3 p-4">
        {detail.isPending ? <Pending /> : <ErrorNote error={detail.error} />}
        {!detail.isPending && <Button onClick={onClose}>Close</Button>}
      </div>
    );
  }
  const status = changeStatus(c);
  return (
    <div className="flex h-full flex-col">
      <div className="flex shrink-0 items-start gap-3 border-b border-line px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="text-xs text-muted">{kindInfo[c.kind]}</div>
          <h2 className="text-base font-semibold [overflow-wrap:anywhere]">
            <a href={c.url} target="_blank" rel="noreferrer" className="group py-3.5 hover:underline md:py-0">
              {c.url}
              <Icon name="external" className="ml-1 inline size-3.5 align-[-2px] text-faint group-hover:text-fg" />
            </a>
          </h2>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted">
            <Badge tone={status.tone}>{status.label}</Badge>
            {!c.verdict && <span>{status.detail}</span>}
          </div>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close" icon>
          <Icon name="x" />
        </Button>
      </div>
      <div className={cx("min-h-0 flex-1 space-y-6 px-4 py-4", scrollArea)}>
        <Facts c={c} zone={zone} />
        <Before c={c} />
        <section className="space-y-3">
          <h3 className="text-sm font-semibold">Result</h3>
          {c.final && <ReadCard read={c.final} />}
          {c.interim && <ReadCard read={c.interim} />}
          {!c.interim && !c.final && (
            <p className="text-sm text-muted">
              {c.first_crawl_at && c.interim_on
                ? `The first result comes on ${fullDay(c.interim_on)} and the final one on ${fullDay(c.verdict_on ?? c.interim_on)}, each comparing the page with the comparison pages.`
                : "Results are timed from when Google first sees the page after the change, so their days are set once it does."}
            </p>
          )}
        </section>
        {detail.isError ? (
          <ErrorNote error={detail.error} />
        ) : !detail.data ? (
          <Pending />
        ) : (
          <>
            <Timeline c={c} steps={detail.data.timeline} zone={zone} />
            <Inspections rows={detail.data.inspections} zone={zone} />
          </>
        )}
        {canEdit && c.source === "manual" && (
          <section className="space-y-2 border-t border-line pt-4">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <p className="text-sm text-muted">This change was logged by hand. Deleting it drops its watch and reads.</p>
              <ConfirmButton label="Delete this change" confirmLabel="Delete change?" onConfirm={() => remove.mutate()} disabled={remove.isPending} className="self-start sm:self-auto">
                <Icon name="trash" className="size-3.5" />
                Delete
              </ConfirmButton>
            </div>
            <ErrorNote error={remove.error} />
          </section>
        )}
      </div>
    </div>
  );
}

/** "#123 Title" linking to the pull request, or "Logged by hand". */
export function ChangeSource({ c, className }: { c: Change; className?: string }) {
  if (c.source === "manual") return <span className={cx("text-muted", className)}>Logged by hand</span>;
  return (
    <a href={c.pr_url} target="_blank" rel="noreferrer" title={`${c.repo}#${c.pr_number} ${c.pr_title}`} className={cx("group hover:underline", className)}>
      <span className="text-muted tabular-nums">#{c.pr_number}</span> {c.pr_title}
    </a>
  );
}

function Facts({ c, zone }: { c: Change; zone: Zone }) {
  const rows: [string, ReactNode][] = [
    ["Change", c.source === "manual" ? "Logged by hand" : <ChangeSource c={c} className="inline-block min-h-11 py-3 [overflow-wrap:anywhere] md:min-h-0 md:py-0" />],
    ...(c.notes ? [["Notes", <span className="whitespace-pre-wrap [overflow-wrap:anywhere]">{c.notes}</span>] as [string, ReactNode]] : []),
    ["Went live", zone.at(c.merged_at)],
    ["Google saw it", c.first_crawl_at ? zone.at(c.first_crawl_at) : <span className="text-muted">Not yet</span>],
    ["First result", c.interim_on ? fullDay(c.interim_on) : <span className="text-muted">Set once Google sees it</span>],
    ["Final result", c.verdict_on ? fullDay(c.verdict_on) : <span className="text-muted">Set once Google sees it</span>],
  ];
  return (
    <section>
      <Card>
        <dl className="divide-y divide-line">
          {rows.map(([k, v]) => (
            <div key={k} className="grid grid-cols-1 gap-0.5 px-4 py-2 text-sm sm:grid-cols-[11rem_minmax(0,1fr)] sm:gap-4">
              <dt className="text-xs font-medium text-muted sm:text-sm sm:font-normal">{k}</dt>
              <dd className="min-w-0">{v}</dd>
            </div>
          ))}
        </dl>
      </Card>
      <p className="mt-1.5 text-xs text-faint">Times are in {zone.zone}, the instance&apos;s time zone. Google counts days in Pacific Time.</p>
    </section>
  );
}

function Before({ c }: { c: Change }) {
  const b = c.before;
  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold">Before the change{b && <span className="font-normal text-muted">, {dayRange(b.from, b.to)}</span>}</h3>
      {b ? (
        <div className="space-y-2">
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {[
              [google.clicks.label, count(b.clicks)],
              [google.impressions.label, count(b.impressions)],
              [google.ctr.label, ctr(b.ctr)],
              [google.position.label, position(b.position)],
            ].map(([label, value]) => (
              <Card key={label} className="px-3 py-2">
                <div className="text-xs text-muted">{label}</div>
                <div className="text-lg font-semibold tabular-nums">{value}</div>
              </Card>
            ))}
          </div>
          <FigureLegend engine="Google" figures={["impressions", "ctr", "position"]} />
        </div>
      ) : (
        <p className="text-sm text-muted">Shows once Google has finished counting the days before the change.</p>
      )}
    </section>
  );
}

/** One result: its outcome, the days behind it, the three moves against their margins, and the comparison pages used. */
export function ReadCard({ read }: { read: ChangeRead }) {
  const o = outcomeInfo[read.outcome];
  const interim = read.kind === "interim";
  const signals = read.signals ?? [];
  const controls = read.controls ?? [];
  return (
    <Card className={cx(interim && "border-dashed")}>
      <div className="flex flex-wrap items-center gap-2 px-4 pt-3">
        <h4 className="text-sm font-semibold">{interim ? "First result" : "Final result"}</h4>
        {interim && <Badge tone="outline">Early</Badge>}
        <Badge tone={o.tone}>{o.label}</Badge>
        <Badge tone="outline">Experimental</Badge>
        <span className="ml-auto text-xs text-muted">Taken {fullDay(read.read_on)}</span>
      </div>
      <div className="space-y-4 px-4 pt-2 pb-4">
        <p className="text-xs text-muted">
          {read.outcome === "insufficient_data" ? `${o.detail} The reason: ${read.reason}.` : o.detail}
          {interim && " The final result can differ."} The method is new: check a surprising result against the page&apos;s numbers on Traffic before acting on it.
        </p>
        {read.after.days > 0 && <Windows before={read.before} after={read.after} />}
        {read.deltas && (
          <div>
            <h5 className="mb-1.5 text-xs font-medium text-muted">Compared with the comparison pages</h5>
            <ul className="divide-y divide-line rounded-surface border border-line">
              {deltaMetrics.map((m) => {
                const s = signals.find((x) => x.metric === m.metric);
                return (
                  <li key={m.metric} className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3 gap-y-0.5 px-3 py-2 text-sm">
                    <span className="min-w-0">
                      <span className="font-medium">{m.label}</span>{" "}
                      <span className={cx("tabular-nums", s ? (s.way === "improved" ? "text-success" : "text-danger") : "text-fg")}>{m.format(m.value(read.deltas!))}</span>
                      <span className="block text-xs text-faint">
                        Counts when it moves more than {m.threshold(read.thresholds)}. {m.reading}
                      </span>
                    </span>
                    {s ? <Badge tone={outcomeInfo[s.way].tone}>{outcomeInfo[s.way].label}</Badge> : <Badge tone="outline">Within margin</Badge>}
                  </li>
                );
              })}
            </ul>
          </div>
        )}
        {controls.length > 0 && (
          <details className="text-xs">
            <summary className="cursor-pointer py-3.5 text-muted hover:text-fg md:py-0">
              Compared with {controls.length} comparison {controls.length === 1 ? "page" : "pages"}
            </summary>
            <ul className="mt-1.5 space-y-0.5">
              {controls.map((u) => (
                <li key={u} className="[overflow-wrap:anywhere]">
                  {u}
                </li>
              ))}
            </ul>
          </details>
        )}
      </div>
    </Card>
  );
}

function Windows({ before, after }: { before: ChangeWindow; after: ChangeWindow }) {
  const cell = "py-1 pr-3 text-right";
  const sides: [string, ChangeWindow][] = [
    ["Before", before],
    ["After", after],
  ];
  return (
    <ScrollX label="days compared">
      <table className="w-full min-w-[24rem] text-left text-xs tabular-nums">
        <thead className="text-muted">
          <tr>
            <th className="py-1 pr-3 font-medium">
              <span className="sr-only">Pages</span>
            </th>
            <th className="py-1 pr-3 text-right font-medium">{google.clicks.label}</th>
            <th className="py-1 pr-3 text-right font-medium">{google.impressions.label}</th>
            <th className="py-1 pr-3 text-right font-medium">{google.ctr.label}</th>
            <th className="py-1 text-right font-medium">{google.position.label}</th>
          </tr>
        </thead>
        {sides.map(([label, win]) => {
          return (
            <tbody key={label} className="border-t border-line">
              <tr>
                <th colSpan={5} className="pt-1.5 pb-0.5 font-medium">
                  {label}, {dayRange(win.from, win.to)} <span className="font-normal text-faint">({win.days} days)</span>
                </th>
              </tr>
              {(
                [
                  ["Page", win.page],
                  ["Comparison pages", win.control],
                ] as const
              ).map(([who, t]) => (
                <tr key={who}>
                  <td className="py-1 pr-3 text-muted">{who}</td>
                  <td className={cell}>{count(t.clicks)}</td>
                  <td className={cell}>{count(t.impressions)}</td>
                  <td className={cell}>{ctr(t.ctr)}</td>
                  <td className="py-1 text-right">{position(t.position)}</td>
                </tr>
              ))}
            </tbody>
          );
        })}
      </table>
    </ScrollX>
  );
}

function stepDetail(c: Change, s: ChangeStep): string {
  if (s.kind === "merged" && c.source === "manual") return c.notes;
  if (s.kind !== "interim" && s.kind !== "verdict") return s.detail;
  return s.done ? (outcomeInfo[s.detail as ChangeOutcome]?.label ?? s.detail) : "Due";
}

function Timeline({ c, steps, zone }: { c: Change; steps: ChangeStep[]; zone: Zone }) {
  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold">Timeline</h3>
      <ol className="space-y-0">
        {steps.map((s, i) => {
          const info = stepInfo[s.kind];
          return (
            <li key={`${s.kind}-${s.at}-${i}`} className="relative flex gap-3 pb-3 last:pb-0">
              {i < steps.length - 1 && <span aria-hidden="true" className="absolute top-6 bottom-0 left-3 w-px bg-line" />}
              <span className={cx("grid size-6 shrink-0 place-items-center rounded-full", s.done ? "bg-subtle-strong text-fg" : "border border-dashed border-line-strong text-faint")}>
                <Icon name={info.icon} className="size-3.5" />
              </span>
              <div className={cx("min-w-0 flex-1 pt-0.5 text-sm", !s.done && "text-muted")}>
                <div className="flex flex-wrap items-baseline gap-x-2">
                  <span className="font-medium">{info.label(c)}</span>
                  <span className="text-xs text-muted">{s.at.length === 10 ? fullDay(s.at) : zone.at(s.at)}</span>
                </div>
                {stepDetail(c, s) && <div className="text-xs text-muted [overflow-wrap:anywhere]">{stepDetail(c, s)}</div>}
              </div>
            </li>
          );
        })}
      </ol>
    </section>
  );
}

function Inspections({ rows, zone }: { rows: UrlInspection[]; zone: Zone }) {
  if (rows.length === 0) return null;
  return (
    <details className="text-sm">
      <summary className="cursor-pointer py-3.5 font-medium text-muted hover:text-fg md:py-0">What Google said about the page ({rows.length})</summary>
      <div className="mt-2">
        <ScrollX label="What Google said about the page">
          <table className="w-full min-w-[32rem] text-left text-xs">
            <thead className="text-muted">
              <tr className="border-b border-line">
                <th className="py-1 pr-3 font-medium">Checked</th>
                <th className="py-1 pr-3 font-medium">Google&apos;s answer</th>
                <th className="py-1 pr-3 font-medium">Status in Google</th>
                <th className="py-1 font-medium">Google last saw it</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {rows.map((r) => (
                <tr key={r.checked_at} className="align-top">
                  <td className="py-1 pr-3 whitespace-nowrap">{zone.at(r.checked_at)}</td>
                  <td className="py-1 pr-3">{r.error ? "–" : r.verdict}</td>
                  <td className="py-1 pr-3 [overflow-wrap:anywhere]">{r.error ? <span className="text-danger">Refused: {r.error}</span> : r.coverage_state}</td>
                  <td className="py-1 whitespace-nowrap">{r.last_crawl_at ? zone.at(r.last_crawl_at) : "–"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollX>
      </div>
    </details>
  );
}
