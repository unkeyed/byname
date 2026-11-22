import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Run, type RunStatus } from "../../api";
import { appCount, checkPrice, nextCheckPhrase } from "../../checks";
import { useToast } from "../../components/Toaster";
import { isOpen, keys, useCosts, useMe, useRuns, useSettings } from "../../queries";
import { Badge, Button, Dialog, DialogBody, ErrorNote, Icon, Pending, Spinner, type Tone } from "../../ui";

export const runStatus: Record<RunStatus, { label: string; tone: Tone }> = {
  queued: { label: "Queued", tone: "info" },
  running: { label: "Checking", tone: "info" },
  completed: { label: "Done", tone: "success" },
  capped: { label: "Stopped at spend cap", tone: "warn" },
  refused: { label: "Refused by Treg", tone: "danger" },
  failed: { label: "Failed", tone: "danger" },
};

export function RunStatusBadge({ run }: { run: Run }) {
  const s = runStatus[run.status];
  return (
    <Badge tone={s.tone} title={run.error || undefined}>
      {isOpen(run) && <Spinner className="size-2.5 border" />}
      {s.label}
    </Badge>
  );
}

/** Whether the signed-in person may queue a check. Viewers see no write actions. */
export const useCanRun = () => useMe().data?.scopes.includes("runs:write") ?? false;

/**
 * Check now asks first, with what the check costs and when the next scheduled
 * one runs, then queues it and polls until it ends and refreshes every result.
 * A check already queued or running is followed instead of starting another.
 */
export function RunNowButton({ variant = "primary" }: { variant?: "primary" | "secondary" }) {
  const qc = useQueryClient();
  const toast = useToast();
  const runs = useRuns();
  const [asking, setAsking] = useState(false);
  const [active, setActive] = useState<number | null>(null);
  const open = runs.data?.find(isOpen);
  const following = active ?? open?.id ?? null;
  const start = useMutation({
    mutationFn: api.runs.runNow,
    onSuccess: (r) => {
      setAsking(false);
      setActive(r.id);
      void qc.invalidateQueries({ queryKey: keys.runs });
    },
  });
  const run = useQuery({
    queryKey: keys.run(following ?? 0),
    queryFn: () => api.runs.get(following!),
    enabled: following != null,
    refetchInterval: (q) => (q.state.data && !isOpen(q.state.data) ? false : 1500),
  });
  const done = run.data && !isOpen(run.data) ? run.data : null;
  const doneKey = done ? `${done.id}:${done.status}` : "";
  useEffect(() => {
    if (!done) return;
    void qc.invalidateQueries({ queryKey: keys.results });
    if (done.id !== active) return;
    setActive(null);
    toast(
      done.status === "completed" ? `Check #${done.id} is done: ${done.succeeded} of ${done.calls} answers came back.` : `Check #${done.id}: ${runStatus[done.status].label.toLowerCase()}. ${done.succeeded} of ${done.calls} answers came back.`,
      done.status === "completed" ? "done" : "error",
    );
    // Once per check that ends, not on every refetch of it.
  }, [doneKey]);

  const busy = start.isPending || (following != null && !done);
  return (
    <div className="flex items-center gap-2">
      {busy && run.data && (
        <span className="hidden text-xs text-muted sm:inline" aria-live="polite">
          Check #{run.data.id}: {run.data.calls} answers so far
        </span>
      )}
      <Button variant={variant} size="sm" disabled={busy} onClick={() => setAsking(true)}>
        {busy ? <Spinner className="size-3.5 border" /> : <Icon name="refresh" className="size-3.5" />}
        Check now
      </Button>
      <Dialog open={asking} onClose={() => setAsking(false)} title="Check now?">
        <ConfirmCheck onConfirm={() => start.mutate()} onCancel={() => setAsking(false)} pending={start.isPending} error={start.error} />
      </Dialog>
    </div>
  );
}

function ConfirmCheck({ onConfirm, onCancel, pending, error }: { onConfirm: () => void; onCancel: () => void; pending: boolean; error: unknown }) {
  const costs = useCosts();
  const settings = useSettings();
  if (costs.isPending || settings.isPending) return <Pending />;
  const c = costs.data;
  const s = settings.data;
  const e = c?.checks.check;
  const price = c ? checkPrice(c) : null;
  return (
    <DialogBody>
      <div className="space-y-3 text-sm">
        {e && price ? (
          <p>
            Ask your {e.questions} {e.questions === 1 ? "question" : "questions"} in {appCount(c)} {appCount(c) === 1 ? "AI app" : "AI apps"} now? That is {e.calls} answers, {price.amount}.
            {price.unknown && <span className="text-muted"> {price.unknown}</span>}
          </p>
        ) : (
          <p>Ask every question in every AI app you chose now?</p>
        )}
        <p className="text-muted">
          {s?.next_check ? `Your next scheduled check still runs ${nextCheckPhrase(s.next_check)}.` : s?.checks_paused ? "Scheduled checks stay paused." : "This does not change the schedule."} Treg bills you for
          the answers, not Byname.
        </p>
        <ErrorNote error={costs.error ?? error} />
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="primary" onClick={onConfirm} disabled={pending} data-autofocus>
            {pending ? <Spinner className="size-3.5 border" /> : <Icon name="refresh" className="size-3.5" />}
            Check now
          </Button>
        </div>
      </div>
    </DialogBody>
  );
}
