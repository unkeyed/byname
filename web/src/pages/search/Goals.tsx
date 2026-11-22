import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, goalMetrics, goalWindows, type Goal, type GoalInput, type GoalMetric, type GoalWindow, type GscStatus } from "../../api";
import { dataVersion, keys } from "../../queries";
import { Badge, Button, Card, CardHeader, ConfirmButton, Dialog, DialogBody, EmptyState, ErrorNote, Field, Icon, inputClass, Pending, selectClass } from "../../ui";
import { count, dayRange, fullDay, metricInfo, windowInfo } from "./model";

/** Goal KPIs: each goal's metric over its trailing window against the target, with add, edit and delete for editors. */
export function GoalsCard({ status, canEdit }: { status: GscStatus; canEdit: boolean }) {
  const qc = useQueryClient();
  const goals = useQuery({ queryKey: keys.goals(dataVersion(status)), queryFn: api.goals.list });
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
          {canEdit ? "Set a target for clicks, impressions or non-brand clicks over 28 days or 3 months, and the date to reach it by." : "Nobody has set a goal. Someone with the Editor or Admin role can add one."}
        </EmptyState>
      </div>
    );
  else
    body = (
      <ul aria-label="Goals" className="grid grid-cols-1 gap-3 px-4 pb-4 sm:grid-cols-2 xl:grid-cols-3">
        {goals.data.map((g) => (
          <GoalTile key={g.id} goal={g} actions={canEdit ? (
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
      <CardHeader title="Goals" description="Each goal counts its metric over the trailing window that ends on the latest final day.">
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
        {editing !== null && <GoalForm goal={editing === "new" ? null : editing} onDone={() => setEditing(null)} />}
      </Dialog>
    </Card>
  );
}

const goalName = (g: GoalInput) => `${metricInfo[g.metric]}, ${windowInfo[g.window_days]}`;

function GoalTile({ goal: g, actions }: { goal: Goal; actions?: React.ReactNode }) {
  const p = g.progress;
  const fraction = p ? Math.min(1, p.fraction) : 0;
  return (
    <li className="flex flex-col gap-1.5 rounded-surface border border-line px-3 py-3">
      <div className="flex items-start justify-between gap-2">
        <h3 className="text-xs font-medium text-muted">{goalName(g)}</h3>
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
          {p.days_with_data < g.window_days && `, only ${p.days_with_data} of ${g.window_days} days stored`}
        </div>
      )}
    </li>
  );
}

function GoalForm({ goal, onDone }: { goal: Goal | null; onDone: () => void }) {
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
    if (valid) save.mutate({ metric, window_days: windowDays, target: n, due_on: due });
  };
  return (
    <DialogBody>
      <form onSubmit={submit} className="space-y-4">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Metric">
            <select className={selectClass + " w-full"} value={metric} onChange={(e) => setMetric(e.target.value as GoalMetric)} disabled={!!goal}>
              {goalMetrics.map((m) => (
                <option key={m} value={m}>
                  {metricInfo[m]}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Window">
            <select className={selectClass + " w-full"} value={windowDays} onChange={(e) => setWindowDays(Number(e.target.value) as GoalWindow)} disabled={!!goal}>
              {goalWindows.map((w) => (
                <option key={w} value={w}>
                  Trailing {windowInfo[w]}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Target" hint={`${metricInfo[metric]} over the trailing ${windowInfo[windowDays]}.`}>
            <input className={inputClass} inputMode="numeric" required value={target} onChange={(e) => setTarget(e.target.value)} placeholder="2400" data-autofocus />
          </Field>
          <Field label="Due">
            <input className={inputClass} type="date" required value={due} onChange={(e) => setDue(e.target.value)} />
          </Field>
        </div>
        {goal ? (
          <p className="text-xs text-muted">To track another metric or window, add a new goal.</p>
        ) : (
          <p className="text-xs text-muted">A goal with the same metric, window and due date is replaced.</p>
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
