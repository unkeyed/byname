import { useEffect, useState } from "react";
import { Link } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type OtherBrand, type OtherBrands, type ResultsFilter } from "../../api";
import { BrandMark } from "../../brandmark";
import { money } from "../../money";
import { keys, useBrandBackfill, useCan, useOtherBrands, useOtherBrandsSettings } from "../../queries";
import { Button, Card, CardHeader, cx, Dialog, DialogBody, EmptyState, ErrorNote, Pending, Spinner } from "../../ui";
import { useView } from "../../view";

/** Where the settings for finding other brands live. */
export const otherBrandsSettings = "/settings/competitors#other-brands";

/** Track and Not a competitor, with what each one refreshes. */
export function useBrandActions() {
  const qc = useQueryClient();
  const refresh = () => Promise.all([qc.invalidateQueries({ queryKey: keys.results }), qc.invalidateQueries({ queryKey: keys.brands }), qc.invalidateQueries({ queryKey: keys.logos })]);
  const track = useMutation({ mutationFn: api.otherBrands.track, onSuccess: refresh });
  const hide = useMutation({ mutationFn: api.otherBrands.hide, onSuccess: refresh });
  const unhide = useMutation({ mutationFn: api.otherBrands.unhide, onSuccess: refresh });
  return { track, hide, unhide, error: track.error ?? hide.error ?? unhide.error };
}

const answers = (n: number) => `${n} ${n === 1 ? "answer" : "answers"}`;

/** "Up 3 since check #11", "First check" or "No change". */
function changeText(b: OtherBrand, previous: number): string {
  if (b.change == null) return "First check";
  if (b.change === 0) return "No change";
  return `${b.change > 0 ? "Up" : "Down"} ${Math.abs(b.change)} since check #${previous}`;
}

const shown = 8;

/** The brands answers in range named that you don't track, with Track and Not a competitor. */
export function AlsoNamed({ filter }: { filter: ResultsFilter }) {
  const list = useOtherBrands(filter);
  const settings = useOtherBrandsSettings();
  const canEdit = useCan("config:write");
  const actions = useBrandActions();
  const [all, setAll] = useState(false);
  const [hiddenOpen, setHiddenOpen] = useState(false);
  if (list.isPending || settings.isPending) return <Pending />;
  if (list.isError) return <ErrorNote error={list.error} />;
  const d = list.data;
  const s = settings.data;
  const on = !!s?.find.on && !!s.has_token;
  const rows = all ? d.brands : d.brands.slice(0, shown);
  return (
    <Card className={cx(list.isPlaceholderData && "opacity-60")}>
      <span id="also-named" className="block scroll-mt-4" />
      <CardHeader
        title="Also named in answers"
        description={
          d.read > 0
            ? `Brands AI answers named that you don't track, from ${d.read} of ${answers(d.answered)} read in this range. They count in no score until you track one.`
            : "Brands AI answers name that you don't track. People new to AI answers rarely know who they compete with there, so Byname reads each answer and lists every brand it names."
        }
      >
        {d.hidden > 0 && (
          <Button size="sm" variant="ghost" aria-expanded={hiddenOpen} onClick={() => setHiddenOpen(!hiddenOpen)}>
            Not competitors ({d.hidden})
          </Button>
        )}
      </CardHeader>
      {hiddenOpen && d.hidden > 0 && <HiddenList canEdit={canEdit} actions={actions} />}
      {d.brands.length === 0 ? (
        <div className="px-4 pb-4">
          <Empty d={d} on={on} />
        </div>
      ) : (
        <>
          <ul className="divide-y divide-line border-t border-line">
            {rows.map((b) => (
              <Row key={b.brand_id} b={b} d={d} canEdit={canEdit} actions={actions} />
            ))}
          </ul>
          {d.brands.length > shown && (
            <div className="border-t border-line px-4 py-1">
              <Button size="sm" variant="ghost" onClick={() => setAll(!all)}>
                {all ? "Show fewer" : `Show all ${d.brands.length}`}
              </Button>
            </div>
          )}
        </>
      )}
      <div className="min-h-5 px-4 pb-3">
        <ErrorNote error={actions.error} />
      </div>
      {d.read < d.answered && <Unread />}
    </Card>
  );
}

function Row({ b, d, canEdit, actions }: { b: OtherBrand; d: OtherBrands; canEdit: boolean; actions: ReturnType<typeof useBrandActions> }) {
  const view = useView();
  const busy = (actions.track.isPending && actions.track.variables === b.brand_id) || (actions.hide.isPending && actions.hide.variables === b.brand_id);
  return (
    <li className="flex flex-col gap-2 px-4 py-3 md:flex-row md:items-start md:gap-4">
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <BrandMark id={b.brand_id} name={b.name} size={28} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <span className="font-medium [overflow-wrap:anywhere]">{b.name}</span>
            {b.domain && <span className="text-xs text-faint">{b.domain}</span>}
          </div>
          <p className="text-sm text-muted">
            Named in{" "}
            <span className="font-medium text-fg tabular-nums">
              {b.named} of {answers(d.answered)}
            </span>
            <span className="text-faint"> · {changeText(b, d.previous_run)}</span>
          </p>
          {b.questions.length > 0 && (
            <ul className="mt-1 space-y-0.5 text-xs text-muted">
              {b.questions.map((q) => (
                <li key={q.question_id}>
                  <button type="button" onClick={() => view.set({ q: q.question_id, qa: null }, { push: true })} className="group flex min-h-11 w-full items-center gap-2 text-left md:min-h-6">
                    <span className="w-20 shrink-0 text-faint tabular-nums">{answers(q.named)}</span>
                    <span className="min-w-0 flex-1 group-hover:text-fg group-hover:underline">“{q.text}”</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
      {canEdit && (
        <div className="flex shrink-0 gap-2 pl-10 md:pl-0">
          <Button size="sm" variant="secondary" disabled={busy} onClick={() => actions.track.mutate(b.brand_id)} aria-label={`Track ${b.name} as a competitor`}>
            {actions.track.isPending && actions.track.variables === b.brand_id ? <Spinner className="size-3.5 border" /> : null}
            Track
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => actions.hide.mutate(b.brand_id)} aria-label={`${b.name} is not a competitor`}>
            Not a competitor
          </Button>
        </div>
      )}
    </li>
  );
}

/** What this list is, before it has anything in it. */
function Empty({ d, on }: { d: OtherBrands; on: boolean }) {
  const canEdit = useCan("config:write");
  if (d.answered === 0)
    return (
      <EmptyState icon="users" title="No answers in this range">
        Once a check has answers, this lists every brand they name that you don't track, so you can see who AI apps suggest instead of you.
      </EmptyState>
    );
  if (!on && d.read === 0)
    return (
      <EmptyState icon="users" title="Finding other brands is off" action={canEdit ? <Link to={otherBrandsSettings} className="text-sm font-medium hover:underline">Turn it on in Settings</Link> : undefined}>
        Byname can read each answer for the brands it names, for a fraction of a cent an answer through Treg, and list the ones you don't track here.
      </EmptyState>
    );
  if (d.read === 0)
    return (
      <EmptyState icon="users" title="Not read yet">
        Byname reads each answer for the brands it names right after a check stores it. The answers in this range have not been read yet: they came before reading was on, or their reads failed and are tried again later.
      </EmptyState>
    );
  return (
    <EmptyState icon="users" title="No other brands named">
      The answers read so far named only you and the competitors you track. New brands show here as soon as an answer names one.
    </EmptyState>
  );
}

/** Answers from before reading was on, with what reading them costs and a button that asks first. */
function Unread() {
  const backfill = useBrandBackfill();
  const canRun = useCan("runs:write");
  const qc = useQueryClient();
  const [asking, setAsking] = useState(false);
  const start = useMutation({
    mutationFn: api.otherBrands.startBackfill,
    onSuccess: () => {
      setAsking(false);
      void qc.invalidateQueries({ queryKey: keys.brandBackfill });
    },
  });
  const b = backfill.data;
  const running = !!b?.latest && (b.latest.status === "queued" || b.latest.status === "running");
  // When a pass ends, the list it filled is read again.
  useEffect(() => {
    if (b?.latest && !running) void qc.invalidateQueries({ queryKey: ["results", "other-brands"] });
  }, [running, b?.latest?.job_id]);
  if (!b || b.unread === 0) return null;
  return (
    <div className="flex flex-col gap-2 border-t border-line bg-subtle/50 px-4 py-3 text-sm sm:flex-row sm:items-center">
      <p className="min-w-0 flex-1 text-muted">
        {running
          ? `Reading past answers: ${b.latest!.read} read so far. The list fills in as it goes.`
          : `${answers(b.unread)} from earlier checks ${b.unread === 1 ? "was" : "were"} never read for other brands. Reading them costs about ${money(b.cost_usd)}, once.`}
      </p>
      {canRun && !running && b.ready && (
        <Button size="sm" variant="secondary" onClick={() => setAsking(true)}>
          Read past answers
        </Button>
      )}
      <Dialog open={asking} onClose={() => setAsking(false)} title="Read past answers?">
        <DialogBody>
          <div className="space-y-3 text-sm">
            <p>
              Read {answers(b.unread)} for the brands they name? That costs about {money(b.cost_usd)}, charged once through Treg and counted against your
              monthly spend cap.
            </p>
            <p className="text-muted">It runs in the background and shows in Check history. Answers from new checks are read as they come in.</p>
            <ErrorNote error={start.error} />
            <div className="flex justify-end gap-2 pt-1">
              <Button variant="ghost" onClick={() => setAsking(false)}>
                Cancel
              </Button>
              <Button variant="primary" onClick={() => start.mutate()} disabled={start.isPending} data-autofocus>
                {start.isPending && <Spinner className="size-3.5 border" />}
                Read {answers(b.unread)}
              </Button>
            </div>
          </div>
        </DialogBody>
      </Dialog>
    </div>
  );
}

/** The brands marked as not a competitor, each with Undo. */
function HiddenList({ canEdit, actions }: { canEdit: boolean; actions: ReturnType<typeof useBrandActions> }) {
  const list = useQuery({ queryKey: keys.hiddenBrands, queryFn: api.otherBrands.hidden });
  if (list.isPending) return <Pending />;
  if (list.isError) return <ErrorNote error={list.error} />;
  return (
    <div className="border-t border-line bg-subtle/50 px-4 py-2">
      <p className="py-1 text-xs text-muted">You marked these as not competitors. They stay out of this list; their answers are still read.</p>
      <ul className="divide-y divide-line">
        {list.data.map((h) => (
          <li key={h.brand_id} className="flex items-center gap-3 py-1.5 text-sm">
            <BrandMark id={h.brand_id} name={h.name} size={20} />
            <span className="min-w-0 flex-1 truncate">{h.name}</span>
            <span className="text-xs text-faint tabular-nums">{answers(h.named)}</span>
            {canEdit && (
              <Button size="sm" variant="ghost" disabled={actions.unhide.isPending} onClick={() => actions.unhide.mutate(h.brand_id)} aria-label={`Put ${h.name} back on the list`}>
                Undo
              </Button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
