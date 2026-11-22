import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Timestamp } from "../api";
import { absoluteTime } from "../format";
import { keys, useCan, useSettings } from "../queries";
import { RunNowButton } from "../pages/results/RunNow";
import { Badge, Button, Card, CardHeader, ErrorNote, Icon, RelativeTime } from "../ui";

function When({ iso, none }: { iso: Timestamp | null; none: string }) {
  if (!iso) return <span className="text-muted">{none}</span>;
  return (
    <span>
      {absoluteTime(iso)} <span className="text-muted">(<RelativeTime iso={iso} />)</span>
    </span>
  );
}

/** Settings > Schedule: when the next and last checks are, with Run now and Pause. */
export function ScheduleStatus() {
  const qc = useQueryClient();
  const settings = useSettings();
  const canRun = useCan("runs:write");
  const toggle = useMutation({
    mutationFn: (pause: boolean) => (pause ? api.checks.pause() : api.checks.resume()),
    onSuccess: (v) => qc.setQueryData(keys.settings, v),
  });
  const s = settings.data;
  return (
    <Card className="mb-8">
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            AI checks {s && (s.checks_paused ? <Badge tone="warn">Paused</Badge> : <Badge tone="success">On schedule</Badge>)}
          </span>
        }
        description="Run now queues a check without moving the schedule. Pause stops scheduled checks and keeps the setting."
      >
        {canRun && s && (
          <>
            <Button size="sm" onClick={() => toggle.mutate(!s.checks_paused)} disabled={toggle.isPending}>
              <Icon name={s.checks_paused ? "refresh" : "x"} className="size-3.5" />
              {s.checks_paused ? "Resume" : "Pause"}
            </Button>
            <RunNowButton variant="secondary" />
          </>
        )}
      </CardHeader>
      <dl className="grid gap-x-6 gap-y-2 border-t border-line px-4 py-3 text-sm sm:grid-cols-[auto_1fr]">
        <dt className="text-muted">Next check</dt>
        <dd>{s?.checks_paused ? <span className="text-muted">Paused</span> : <When iso={s?.next_check_at ?? null} none="Not scheduled yet" />}</dd>
        <dt className="text-muted">Last check</dt>
        <dd>
          <When iso={s?.last_check ? (s.last_check.started_at ?? s.last_check.queued_at) : null} none="None yet" />
        </dd>
      </dl>
      {(settings.error || toggle.error) && (
        <div className="px-4 pb-3">
          <ErrorNote error={settings.error ?? toggle.error} />
        </div>
      )}
    </Card>
  );
}
