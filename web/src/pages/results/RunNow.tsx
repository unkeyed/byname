import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Run, type RunStatus } from "../../api";
import { useToast } from "../../components/Toaster";
import { isOpen, keys, useMe, useRuns } from "../../queries";
import { Badge, Button, ErrorNote, Icon, Spinner, type Tone } from "../../ui";

export const runStatus: Record<RunStatus, { label: string; tone: Tone }> = {
  queued: { label: "Queued", tone: "info" },
  running: { label: "Running", tone: "info" },
  completed: { label: "Completed", tone: "success" },
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
 * Queues a check and polls it until it ends, then refreshes every result.
 * A check already queued or running is followed instead of starting another.
 */
export function RunNowButton({ variant = "primary" }: { variant?: "primary" | "secondary" }) {
  const qc = useQueryClient();
  const toast = useToast();
  const runs = useRuns();
  const [active, setActive] = useState<number | null>(null);
  const open = runs.data?.find(isOpen);
  const following = active ?? open?.id ?? null;
  const start = useMutation({
    mutationFn: api.runs.runNow,
    onSuccess: (r) => {
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
    toast(`Check #${done.id} ${runStatus[done.status].label.toLowerCase()}: ${done.succeeded} of ${done.calls} calls answered.`, done.status === "completed" ? "done" : "error");
    // Once per run that ends, not on every refetch of it.
  }, [doneKey]);

  const busy = start.isPending || (following != null && !done);
  return (
    <div className="flex items-center gap-2">
      {busy && run.data && (
        <span className="hidden text-xs text-muted sm:inline" aria-live="polite">
          Check #{run.data.id} {run.data.status}, {run.data.calls} calls so far
        </span>
      )}
      <Button variant={variant} size="sm" disabled={busy} onClick={() => start.mutate()}>
        {busy ? <Spinner className="size-3 border" /> : <Icon name="refresh" className="size-3.5" />}
        Run now
      </Button>
      {start.error && <ErrorNote error={start.error} />}
    </div>
  );
}
