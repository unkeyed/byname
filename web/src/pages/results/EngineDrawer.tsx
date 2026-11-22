import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api, type EnginePrompt, type Fidelity, type Line, type Series } from "../../api";
import { LineChart } from "../../charts";
import { fidelityInfo, palette, pct, shortDay, useLabels } from "../../lines";
import { keys, useSurfaces } from "../../queries";
import { Badge, Button, Card, cx, ErrorNote, Icon, LoadBoundary, Pending, scrollArea, useModal } from "../../ui";
import { useFilter, useView } from "../../view";
import { Instead } from "./Questions";

const fidelities: Fidelity[] = ["app", "model_match", "approximation"];

/** A line as ?eng= carries it: surface~pair~endpoint~fidelity, pair 0 for a surface line. */
export const engineParam = (s: Series) => [s.pair.surface, s.pair.id, s.endpoint, s.fidelity].join("~");

function parseEngine(v: string): Line | null {
  const [surface, pair, endpoint, fidelity] = v.split("~");
  if (!surface || !endpoint || !fidelities.includes(fidelity as Fidelity) || !/^\d+$/.test(pair ?? "")) return null;
  return { surface, pair: Number(pair), endpoint, fidelity: fidelity as Fidelity };
}

/** Opens the engine drawer on a line, as a link that Back closes. */
export function useOpenEngine() {
  const view = useView();
  return (s: Series) => view.set({ eng: engineParam(s) }, { push: true });
}

/** One AI app, or one AI app and model: open wherever ?eng= names one. */
export function EngineDrawer() {
  const view = useView();
  const line = parseEngine(view.get("eng"));
  const ref = useModal(line !== null);
  const close = () => view.set({ eng: null });
  return (
    <dialog
      ref={ref}
      onClose={close}
      onClick={(e) => e.target === ref.current && close()}
      aria-label="AI app"
      data-drawer
      className="m-0 ml-auto h-dvh max-h-none w-full max-w-3xl border-0 border-l border-line bg-bg p-0 text-fg shadow-floating"
    >
      {line && (
        <LoadBoundary key={view.get("eng")} className="h-full">
          <Body line={line} onClose={close} />
        </LoadBoundary>
      )}
    </dialog>
  );
}

function Kpi({ label, value, detail }: { label: string; value: string; detail: ReactNode }) {
  return (
    <Card className="flex flex-col gap-1 px-3 py-3">
      <h3 className="text-xs font-medium text-muted">{label}</h3>
      <span className="text-2xl font-semibold tabular-nums">{value}</span>
      <span className="text-xs text-muted">{detail}</span>
    </Card>
  );
}

function Body({ line, onClose }: { line: Line; onClose: () => void }) {
  const view = useView();
  const { filter } = useFilter();
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const color = palette(surfaces.data);
  const engine = useQuery({ queryKey: keys.engine(line, filter.since), queryFn: () => api.engine(line, filter.since) });
  const openQuestion = (p: EnginePrompt) => view.set({ q: p.question.id, qa: p.answer_id, qv: null }, { push: true });
  const title = engine.data ? labels.line(engine.data.pair, engine.data.models) : labels.line({ id: line.pair, surface: line.surface, model: "" });

  let body;
  if (engine.isPending) body = <Pending />;
  else if (engine.isError) body = <ErrorNote error={engine.error} />;
  else if (engine.data.runs === 0) body = <p className="text-sm text-muted">This AI app has no answers in this range. Widen the date range in the page header.</p>;
  else {
    const e = engine.data;
    const xs = e.points.map((p) => ({ key: String(p.run_id), label: `#${p.run_id}`, title: `Check #${p.run_id}, ${shortDay(p.run_started_at)}` }));
    const ranked = e.ranking.filter((b) => b.mentions > 0 || b.kind === "self");
    const top = ranked.slice(0, 10);
    const self = ranked.find((b) => b.kind === "self");
    if (self && !top.includes(self)) top.push(self);
    const max = Math.max(...top.map((b) => b.mentions), 1);
    body = (
      <div className="space-y-6">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Kpi label="Named you" value={pct(e.visibility)} detail={`${e.named} of ${e.answered} answers named you in check #${e.latest_run_id}`} />
          <Kpi label="Questions" value={`${e.visible_prompts} of ${e.prompts}`} detail="questions where it named you" />
          <Kpi label="Average position" value={e.average_position == null ? "–" : `#${e.average_position.toFixed(1)}`} detail={e.average_position == null ? "It did not name you in the latest check" : "Among the brands it named. #1 is first."} />
        </div>

        <section>
          <h3 className="mb-2 text-sm font-semibold">Named you, by check</h3>
          <Card className="px-3 py-3">
            <LineChart
              xs={xs}
              lines={[{ key: "v", label: title, color: color(e.pair), approx: e.fidelity === "approximation", values: e.points.map((p) => p.mention_rate) }]}
              format={(v) => pct(v)}
              height={180}
              label={`How often ${title} named you, by check`}
            />
          </Card>
        </section>

        <section>
          <h3 className="mb-2 text-sm font-semibold">Brands it names most</h3>
          <p className="mb-2 text-xs text-muted">
            Answers from this AI app naming each brand over {e.runs} {e.runs === 1 ? "check" : "checks"}. Technologies are left out.
          </p>
          {top.length === 0 ? (
            <p className="text-sm text-muted">No brand was named.</p>
          ) : (
            <ol className="space-y-1.5">
              {top.map((b) => (
                <li key={b.brand_id} className="grid grid-cols-[2rem_minmax(0,9rem)_minmax(0,1fr)_2.5rem] items-center gap-2 text-xs">
                  <span className="text-muted tabular-nums">{b.rank ? `#${b.rank}` : "–"}</span>
                  <span className={cx("truncate", b.kind === "self" ? "font-semibold" : "text-muted")}>
                    {b.name}
                    {b.kind === "self" && " (you)"}
                  </span>
                  <span className="h-2 rounded-full bg-subtle">
                    <span className="block h-2 rounded-full" style={{ width: `${(b.mentions / max) * 100}%`, background: b.kind === "self" ? "var(--you)" : "var(--line-hover)" }} />
                  </span>
                  <span className="text-right font-medium tabular-nums">{b.mentions}</span>
                </li>
              ))}
            </ol>
          )}
        </section>

        <QuestionSection
          title={`Did not name you in ${e.not_mentioned.length} ${e.not_mentioned.length === 1 ? "question" : "questions"}`}
          empty="It named you in every question in the latest check."
          prompts={e.not_mentioned}
          onOpen={openQuestion}
          aside={(p) => (
            <span className="flex flex-wrap items-center justify-end gap-1">
              {p.cited && <Badge tone="info">Linked</Badge>}
              <Instead names={p.instead} />
            </span>
          )}
        />
        <QuestionSection
          title={`Named you in ${e.visible.length} ${e.visible.length === 1 ? "question" : "questions"}`}
          empty="It did not name you in any question in the latest check."
          prompts={e.visible}
          onOpen={openQuestion}
          aside={(p) => <span className="inline-flex h-5 items-center rounded-control bg-success-soft px-1.5 text-xs font-semibold text-success tabular-nums">#{p.position}</span>}
        />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex shrink-0 items-start gap-3 border-b border-line px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="text-xs text-muted">{line.pair ? "AI app and model" : "AI app"}</div>
          <h2 className="text-base font-semibold [overflow-wrap:anywhere]">{title}</h2>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted">
            <Badge tone={line.fidelity === "approximation" ? "warn" : "outline"}>{fidelityInfo[line.fidelity].label}</Badge>
            <span>{fidelityInfo[line.fidelity].detail}</span>
          </div>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close" icon>
          <Icon name="x" />
        </Button>
      </div>
      <div className={cx("min-h-0 flex-1 px-4 py-4", scrollArea)}>{body}</div>
    </div>
  );
}

function QuestionSection({ title, empty, prompts, onOpen, aside }: { title: string; empty: string; prompts: EnginePrompt[]; onOpen: (p: EnginePrompt) => void; aside: (p: EnginePrompt) => ReactNode }) {
  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold">{title}</h3>
      {prompts.length === 0 ? (
        <p className="text-sm text-muted">{empty}</p>
      ) : (
        <ul className="divide-y divide-line rounded-surface border border-line bg-surface">
          {prompts.map((p) => (
            <li key={p.question.id}>
              <button type="button" onClick={() => onOpen(p)} className="flex w-full flex-col gap-1.5 px-3 py-3 text-left hover:bg-subtle md:py-2 sm:flex-row sm:items-center sm:gap-3">
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium">{p.question.text}</span>
                  <span className="block text-xs text-faint">{p.question.group}</span>
                </span>
                <span className="shrink-0">{aside(p)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
