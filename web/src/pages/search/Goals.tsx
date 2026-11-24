import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, goalMetrics, goalWindows, type Goal, type GoalInput, type GoalMetric, type GoalWindow, type SearchStatus } from "../../api";
import { dataVersion, keys } from "../../queries";
import { Badge, Button, Card, CardHeader, ConfirmButton, Dialog, DialogBody, EmptyState, ErrorNote, Field, Icon, inputClass, Pending, selectClass } from "../../ui";
import { count, dayRange, fullDay, metricHint, metricInfo, windowInfo } from "./model";

/** One engine's goal KPIs: each goal's metric over its trailing window against the target, with add, edit and delete for editors. */
export function GoalsCard({ status, canEdit }: { status: SearchStatus; canEdit: boolean }) {
  const qc = useQueryClient();
  const goals = useQuery({ queryKey: keys.goals(dataVersion(status)), queryFn: api.goals.list, select: (all) => all.filter((g) => g.provider === status.provider) });
  const [editing, setEditing] = useState<Goal | "new" | null>(null);
  const remove = useMutation({ mutationFn: api.goals.remove, onSuccess: () => qc.invalidateQueries({ queryKey: ["search", "goals"] }) });

  let body;
  if (goals.isPending) body = <Pending />;
  else if (goals.isError) body = <div className="px-4 pb-4"><ErrorNote error={goals.error} /></div>;
  else if (goals.data.length === 0)
    body = (
      <div className="px-4 pb-4">
        <EmptyState
          icon="chart"
          title="No goals yet"
          action={
            canEdit ? (
              <Button variant="primary" onClick={() => setEditing("new")}>
                <Icon name="plus" className="size-3.5" />
                Add a goal
              </Button>
            ) : undefined
          }
        >
          {canEdit
            ? `Set a target for ${status.title} clicks, appearances or searches without your name over 28 days or 3 months, and the date to reach it by.`
            : `Nobody has set a goal for ${status.title}. Someone with the Editor or Admin role can add one.`}
        </EmptyState>
      </div>
    );
  else
    body = (
      <ul aria-label="Goals" className="grid grid-cols-1 gap-3 px-4 pb-4 sm:grid-cols-2 xl:grid-cols-3">
        {goals.data.map((g) => (
          <GoalTile key={g.id} goal={g} engine={status.title} actions={canEdit ? (
            <>
              <Button size="sm" variant="ghost" onClick={() => setEditing(g)} aria-label={`Edit ${goalName(g)}`} title="Edit" icon>
                <Icon name="pencil" className="size-3.5" />
              </Button>
              <ConfirmButton label={`Delete ${goalName(g)}`} confirmLabel="Delete?" onConfirm={() => remove.mutate(g.id)} disabled={remove.isPending}>
                <Icon name="trash" className="size-3.5" />
              </ConfirmButton>
            </>
          ) : undefined} />
        ))}
      </ul>
    );

  return (
    <Card>
      <CardHeader
        title="Goals"
        description={
          status.step_days > 1
            ? `Targets you set for ${status.title}. Each goal adds up whole weeks, 4 or 13, up to the end of the latest week ${status.title} has finished counting.`
            : `Targets you set for ${status.title}. Each goal adds up the last 28 days or 3 months, up to the latest day ${status.title} has finished counting.`
        }
      >
        {canEdit && goals.data && goals.data.length > 0 && (
          <Button size="sm" onClick={() => setEditing("new")}>
            <Icon name="plus" className="size-3.5" />
            Add goal
          </Button>
        )}
      </CardHeader>
      {body}
      {remove.error != null && <div className="px-4 pb-4"><ErrorNote error={remove.error} /></div>}
      <Dialog open={editing !== null} onClose={() => setEditing(null)} title={editing === "new" ? "Add a goal" : "Edit goal"}>
        {editing !== null && <GoalForm status={status} goal={editing === "new" ? null : editing} onDone={() => setEditing(null)} />}
      </Dialog>
    </Card>
  );
}

const goalName = (g: GoalInput) => `${metricInfo[g.metric]}, ${windowInfo[g.window_days]}`;

function GoalTile({ goal: g, engine, actions }: { goal: Goal; engine: string; actions?: React.ReactNode }) {
  const p = g.progress;
  const days = p ? Math.round((Date.parse(p.to) - Date.parse(p.from)) / 86_400_000) + 1 : g.window_days;
  const fraction = p ? Math.min(1, p.fraction) : 0;
  return (
    <li className="flex flex-col gap-1.5 rounded-surface border border-line px-3 py-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="text-xs font-medium text-muted">{goalName(g)}</h3>
          <p className="text-xs text-faint">{metricHint(engine, g.metric)}</p>
        </div>
        {actions && <div className="-my-1 -mr-1 flex shrink-0 items-center">{actions}</div>}
      </div>
      <div className="flex flex-wrap items-baseline gap-x-1.5">
        <span className="text-2xl font-semibold">{p ? count(p.value) : "–"}</span>
        <span className="text-sm text-muted">of {count(g.target)}</span>
        {p?.reached && (
          <Badge tone="success" className="ml-auto self-center">
            <Icon name="check" className="size-3" />
            Reached
          </Badge>
        )}
      </div>
      <div
        role="meter"
        aria-label={`${goalName(g)}: ${p ? Math.round(p.fraction * 100) : 0}% of target`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(fraction * 100)}
        className="h-1.5 rounded-full bg-subtle"
      >
        <div className="h-1.5 rounded-full" style={{ width: `${fraction * 100}%`, background: p?.reached ? "var(--success)" : "var(--you)" }} />
      </div>
      <div className="flex flex-wrap justify-between gap-x-2 text-xs text-muted">
        <span>{p ? `${Math.round(p.fraction * 100)}% of target` : "No data yet"}</span>
        <span>Due {fullDay(g.due_on)}</span>
      </div>
      {p && (
        <div className="text-xs text-faint">
          {dayRange(p.from, p.to)}
          {p.days_with_data < days && `, only ${p.days_with_data} of ${days} days in so far`}
        </div>
      )}
    </li>
  );
}

function GoalForm({ status, goal, onDone }: { status: SearchStatus; goal: Goal | null; onDone: () => void }) {
  const qc = useQueryClient();
  const [metric, setMetric] = useState<GoalMetric>(goal?.metric ?? "clicks");
  const [windowDays, setWindowDays] = useState<GoalWindow>(goal?.window_days ?? 28);
  const [target, setTarget] = useState(goal ? String(goal.target) : "");
  const [due, setDue] = useState(goal?.due_on ?? "");
  const save = useMutation({
    mutationFn: (input: GoalInput) => (goal ? api.goals.update(goal.id, input) : api.goals.create(input)),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["search", "goals"] });
      onDone();
    },
  });
  const n = Number(target.replace(/[\s,]/g, ""));
  const valid = Number.isInteger(n) && n >= 1 && /^\d{4}-\d{2}-\d{2}$/.test(due);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (valid) save.mutate({ provider: status.provider, metric, window_days: windowDays, target: n, due_on: due });
  };
  return (
    <DialogBody>
      <form onSubmit={submit} className="space-y-4">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="What to count" hint={metricHint(status.title, metric)}>
            <select className={selectClass + " w-full"} value={metric} onChange={(e) => setMetric(e.target.value as GoalMetric)} disabled={!!goal}>
              {goalMetrics.map((m) => (
                <option key={m} value={m}>
                  {metricInfo[m]}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Over">
            <select className={selectClass + " w-full"} value={windowDays} onChange={(e) => setWindowDays(Number(e.target.value) as GoalWindow)} disabled={!!goal}>
              {goalWindows.map((w) => (
                <option key={w} value={w}>
                  The last {windowInfo[w]}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Target" hint={`${metricInfo[metric]} from ${status.title} over the last ${windowInfo[windowDays]}${status.step_days > 1 && windowDays === 90 ? ", 13 whole weeks" : ""}.`}>
            <input className={inputClass} inputMode="numeric" required value={target} onChange={(e) => setTarget(e.target.value)} placeholder="2400" data-autofocus />
          </Field>
          <Field label="Due">
            <input className={inputClass} type="date" required value={due} onChange={(e) => setDue(e.target.value)} />
          </Field>
        </div>
        {goal ? (
          <p className="text-xs text-muted">To count something else or over another span, add a new goal.</p>
        ) : (
          <p className="text-xs text-muted">A goal that counts the same thing over the same span with the same due date is replaced.</p>
        )}
        <div className="flex justify-end gap-2">
          <Button onClick={onDone}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={!valid || save.isPending}>
            {goal ? "Save goal" : "Add goal"}
          </Button>
        </div>
        <ErrorNote error={save.error} />
      </form>
    </DialogBody>
  );
}
