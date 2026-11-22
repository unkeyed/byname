import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, type Alert } from "../api";
import { keys, useAlerts, useMe } from "../queries";
import { Button, buttonClass, Card, cx, EmptyState, ErrorNote, Icon, PageHeader, Pending, RelativeTime } from "../ui";

/** The alerts indicator: a bell with the unread count, linking to the alerts list. */
export function AlertsBell({ className, onNavigate }: { className?: string; onNavigate?: () => void }) {
  const alerts = useAlerts();
  const unread = alerts.data?.unread ?? 0;
  const label = unread ? `Alerts, ${unread} unread` : "Alerts";
  return (
    <Link to="/alerts" onClick={onNavigate} aria-label={label} title={label} className={cx(buttonClass({ variant: "ghost", size: "sm", icon: true }), "relative", className)}>
      <BellIcon />
      {unread > 0 && (
        <span className="absolute -top-0.5 -right-0.5 grid h-4 min-w-4 place-items-center rounded-full bg-danger px-1 text-[0.625rem] leading-none font-semibold text-surface tabular-nums">
          {unread > 99 ? "99+" : unread}
        </span>
      )}
    </Link>
  );
}

function BellIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.75} strokeLinecap="round" strokeLinejoin="round" className="size-4 shrink-0" aria-hidden="true">
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </svg>
  );
}

/** Every alert this person may see, newest first. Opening one marks it read. */
export function Alerts() {
  const qc = useQueryClient();
  const me = useMe();
  const alerts = useAlerts();
  const read = useMutation({
    mutationFn: api.alerts.read,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.alerts }),
  });
  const unread = alerts.data?.unread ?? 0;
  return (
    <>
      <PageHeader title="Alerts">
        {unread > 0 && (
          <Button size="sm" onClick={() => read.mutate({ all: true })} disabled={read.isPending}>
            <Icon name="check" className="size-3.5" />
            Mark all read
          </Button>
        )}
      </PageHeader>
      <div className="relative min-h-0 flex-1 overflow-auto">
        <div className="mx-auto max-w-3xl space-y-3 px-4 py-6 pb-16 md:px-8">
          <p className="text-sm text-muted">
            Byname raises an alert when a check finishes, stops or reaches its spend cap, and when a model leaves its list.
            {me.data?.role === "admin" ? (
              <>
                {" "}
                Send them by email, to Slack or to a webhook under{" "}
                <Link to="/settings/notifications" className="text-fg underline underline-offset-2">
                  Settings, Notifications
                </Link>
                .
              </>
            ) : (
              " An admin can also send them by email, to Slack or to a webhook."
            )}
          </p>
          <ErrorNote error={alerts.error ?? read.error} />
          {alerts.isPending ? (
            <Pending />
          ) : alerts.data?.alerts.length === 0 ? (
            <EmptyState icon="check" title="No alerts yet">
              The first one arrives when a check finishes.
            </EmptyState>
          ) : (
            <Card>
              <ul aria-label="Alerts" className="divide-y divide-line">
                {alerts.data?.alerts.map((a) => (
                  <AlertRow key={a.id} alert={a} onOpen={() => !a.read && read.mutate({ ids: [a.id] })} />
                ))}
              </ul>
            </Card>
          )}
        </div>
      </div>
    </>
  );
}

function AlertRow({ alert: a, onOpen }: { alert: Alert; onOpen: () => void }) {
  return (
    <li>
      <Link to={a.path} onClick={onOpen} className="flex gap-3 px-4 py-3 transition-colors hover:bg-subtle">
        <span aria-hidden="true" className={cx("mt-1.5 size-2 shrink-0 rounded-full", a.read ? "bg-transparent" : "bg-info")} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline justify-between gap-x-3">
            <span className={cx("min-w-0 [overflow-wrap:anywhere]", a.read ? "text-muted" : "font-medium")}>
              {a.title}
              {!a.read && <span className="sr-only"> (unread)</span>}
            </span>
            <RelativeTime iso={a.created_at} className="shrink-0 text-xs text-muted" />
          </div>
          {a.lines.length > 0 && (
            <ul className="mt-1 space-y-0.5 text-xs text-muted">
              {a.lines.map((l, i) => (
                <li key={i} className="[overflow-wrap:anywhere]">
                  {l}
                </li>
              ))}
            </ul>
          )}
        </div>
        <Icon name="right" className="mt-1 size-3.5 text-faint" />
      </Link>
    </li>
  );
}
