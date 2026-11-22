import { useState } from "react";
import { Link } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Alert, type AlertList } from "../../api";
import { keys, useIsAdmin, useNotifications, useRuns, useSurfaces } from "../../queries";
import { Button, EmptyState, ErrorNote, errorMessage, Icon, PageHeader, Pending, useLoading } from "../../ui";
import { useToast } from "../../components/Toaster";
import { Segmented } from "../results/Header";
import { NotificationCard, type CardContext } from "./cards";

type Show = "all" | "unread";

/**
 * Marks notifications read at once, so the bell and the dots change with the
 * click, then confirms with the server. Opening a card leaves the page, so a
 * failure shows as a toast.
 */
function useMarkRead() {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: api.alerts.read,
    onMutate: (body) => {
      const read = (a: Alert) => "all" in body || body.ids.includes(a.id);
      qc.setQueryData<AlertList>(keys.notifications, (l) => {
        if (!l) return l;
        const alerts = l.alerts.map((a) => (read(a) ? { ...a, read: true } : a));
        return { alerts, unread: Math.max(0, l.unread - l.alerts.filter((a) => !a.read && read(a)).length) };
      });
    },
    onError: (err) => toast(errorMessage(err), "error"),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.notifications }),
  });
}

/** Every notification this person may see, newest first, grouped by day, as cards built from each one's data. */
export function Notifications() {
  const admin = useIsAdmin();
  const list = useNotifications();
  const runs = useRuns();
  const surfaces = useSurfaces();
  const read = useMarkRead();
  const [show, setShow] = useState<Show>("all");
  useLoading(runs.isPending || surfaces.isPending);
  const ctx: CardContext = { runs: runs.data ?? [], surfaces: surfaces.data ?? [] };
  const unread = list.data?.unread ?? 0;
  const shown = list.data?.alerts.filter((a) => show === "all" || !a.read) ?? [];

  let body;
  if (list.isPending) body = <Pending />;
  else if (list.isError) body = <ErrorNote error={list.error} />;
  else if (list.data.alerts.length === 0)
    body = (
      <EmptyState icon="bell" title="No notifications yet">
        The first one arrives when a check finishes.
      </EmptyState>
    );
  else if (shown.length === 0)
    body = (
      <EmptyState icon="check" title="You have read them all">
        New ones show here and on the bell.
      </EmptyState>
    );
  else
    body = groups(shown).map((g) => (
      <section key={g.label} aria-labelledby={`day-${g.key}`} className="space-y-2">
        <h2 id={`day-${g.key}`} className="text-xs font-medium text-muted">
          {g.label}
        </h2>
        <ul className="space-y-2">
          {g.items.map((a) => (
            <NotificationCard key={a.id} alert={a} ctx={ctx} onOpen={() => !a.read && read.mutate({ ids: [a.id] })} />
          ))}
        </ul>
      </section>
    ));

  return (
    <>
      <PageHeader title="Notifications" />
      <div className="relative min-h-0 flex-1 overflow-auto">
        <div className="mx-auto max-w-2xl space-y-5 px-4 py-6 pb-16 md:px-8">
          <p className="text-sm text-muted">
            What Byname noticed. Each one links to the details.
            {admin && (
              <>
                {" "}
                Choose where else they go in{" "}
                <Link to="/settings/notifications" className="text-fg underline underline-offset-2">
                  Settings, Notifications
                </Link>
                .
              </>
            )}
          </p>
          <div className="flex items-center justify-between gap-3">
            <Segmented
              label="Show"
              value={show}
              onChange={setShow}
              options={[
                { id: "all", label: "All" },
                { id: "unread", label: "Unread" },
              ]}
            />
            <Button size="sm" onClick={() => read.mutate({ all: true })} disabled={unread === 0}>
              <Icon name="check" className="size-3.5" />
              Mark all read
            </Button>
          </div>
          {body}
        </div>
      </div>
    </>
  );
}

type Group = { key: string; label: string; items: Alert[] };

/** Today, Yesterday, This week (the five days before) and Earlier, by the reader's calendar. The list is newest first, so each group is too. */
function groups(alerts: Alert[], now = new Date()): Group[] {
  const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const day = 86_400_000;
  const buckets: Group[] = [
    { key: "today", label: "Today", items: [] },
    { key: "yesterday", label: "Yesterday", items: [] },
    { key: "week", label: "This week", items: [] },
    { key: "earlier", label: "Earlier", items: [] },
  ];
  for (const a of alerts) {
    const t = new Date(a.created_at).getTime();
    const i = t >= midnight ? 0 : t >= midnight - day ? 1 : t >= midnight - 6 * day ? 2 : 3;
    buckets[i].items.push(a);
  }
  return buckets.filter((b) => b.items.length > 0);
}
