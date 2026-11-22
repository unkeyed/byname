import { useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type ChannelInput, type ChannelKind, type CreatedChannel, type Delivery, type EventKind, type EventKindName, type NotificationChannel, type SMTPTLS } from "../../api";
import { keys } from "../../queries";
import {
  Badge,
  Button,
  Card,
  CardHeader,
  ConfirmButton,
  CopyField,
  cx,
  Dialog,
  DialogBody,
  EmptyState,
  ErrorNote,
  Field,
  Icon,
  inputClass,
  Pending,
  RelativeTime,
  selectClass,
  type IconName,
  type Tone,
} from "../../ui";
import { ReauthProvider, useReauthMutation } from "./Reauth";
import { SettingsHeader } from "./Settings";

/** Each channel kind as Settings names and draws it. */
const kinds: Record<ChannelKind, { label: string; icon: IconName; blurb: string }> = {
  smtp: { label: "Email (SMTP)", icon: "mail", blurb: "Your own mail server or a provider's SMTP relay." },
  email_api: { label: "Email (Resend)", icon: "mail", blurb: "Resend's HTTP API with an API key. Resend is the one email API Byname sends through." },
  slack_webhook: { label: "Slack", icon: "message", blurb: "A Slack incoming webhook posts to one channel." },
  webhook: { label: "Webhook", icon: "link", blurb: "A signed JSON POST to your own endpoint." },
};
const kindOrder: ChannelKind[] = ["smtp", "email_api", "slack_webhook", "webhook"];

/** What a new place gets: every alert about checks and models. */
const defaultEvents: EventKindName[] = ["check_completed", "check_refused", "check_failed", "spend_cap_reached", "pair_paused"];

const tlsModes: { value: SMTPTLS; label: string }[] = [
  { value: "starttls", label: "STARTTLS, port 587" },
  { value: "tls", label: "TLS, port 465" },
  { value: "none", label: "None, local relay" },
];

const useChannels = () => useQuery({ queryKey: keys.channels, queryFn: api.channels.list });

export function Notifications() {
  return (
    <>
      <SettingsHeader title="Alerts">
        Alerts tell you what changed, and every one shows in the app under the bell. Choose where else they go: an email address, a Slack channel or a webhook. Each place
        gets only the alerts you tick.
      </SettingsHeader>
      <Channels />
      <DeliveryLog />
    </>
  );
}

function Channels() {
  const list = useChannels();
  const [editing, setEditing] = useState<NotificationChannel | "new" | null>(null);
  if (list.isPending) return <Pending />;
  if (list.isError) return <ErrorNote error={list.error} />;
  const { channels, events } = list.data;
  return (
    <>
      <div className="mb-3 flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold">Where alerts go</h3>
        <Button variant="primary" onClick={() => setEditing("new")}>
          <Icon name="plus" className="size-3.5" />
          Add a place
        </Button>
      </div>
      {channels.length === 0 ? (
        <EmptyState icon="mail" title="Alerts show in the app only">
          Add an email address, Slack or a webhook to send them where your team already looks.
        </EmptyState>
      ) : (
        <Card>
          <ul aria-label="Where alerts go" className="divide-y divide-line">
            {channels.map((c) => (
              <ChannelRow key={c.id} channel={c} events={events} onEdit={() => setEditing(c)} />
            ))}
          </ul>
        </Card>
      )}
      <Dialog open={editing !== null} onClose={() => setEditing(null)} title={editing === "new" ? "Add a place to send alerts" : `Edit ${editing?.name ?? ""}`} wide>
        {editing !== null && <ChannelForm channel={editing === "new" ? undefined : editing} events={events} onDone={() => setEditing(null)} />}
      </Dialog>
    </>
  );
}

/** One line saying where a channel sends, without its secrets. */
export function channelTarget(c: NotificationChannel): string {
  const s = c.settings;
  const to = s.to?.length ? s.to[0] + (s.to.length > 1 ? `, +${s.to.length - 1}` : "") : "";
  switch (c.kind) {
    case "smtp":
      return `${s.host}:${s.port} to ${to}`;
    case "email_api":
      return `From ${s.from} to ${to}`;
    case "slack_webhook":
      return "Incoming webhook";
    case "webhook":
      return s.url ?? "";
  }
}

function TestStatus({ c }: { c: NotificationChannel }) {
  if (!c.last_test_at) return <span className="text-faint">Not tested yet</span>;
  const ok = c.last_test_status === "ok";
  return (
    <span className={cx("[overflow-wrap:anywhere]", ok ? "text-success" : "text-danger")}>
      {ok ? "Test sent" : "Test failed"} <RelativeTime iso={c.last_test_at} />
      {!ok && <>: {c.last_test_status}</>}
    </span>
  );
}

function ChannelRow({ channel: c, events, onEdit }: { channel: NotificationChannel; events: EventKind[]; onEdit: () => void }) {
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: keys.channels });
  const test = useMutation({ mutationFn: () => api.channels.test(c.id), onSuccess: refresh });
  const remove = useMutation({ mutationFn: () => api.channels.remove(c.id), onSuccess: refresh });
  const titles = new Map(events.map((e) => [e.kind, e.title]));
  return (
    <li className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-start">
      <span className="grid size-8 shrink-0 place-items-center rounded-control bg-subtle text-muted max-sm:hidden">
        <Icon name={kinds[c.kind].icon} className="size-4" />
      </span>
      <div className="min-w-0 flex-1 space-y-1.5">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="min-w-0 font-medium [overflow-wrap:anywhere]">{c.name}</span>
          <Badge tone="outline">{kinds[c.kind].label}</Badge>
          {!c.enabled && <Badge tone="warn">Off</Badge>}
        </div>
        <div className="text-xs text-muted [overflow-wrap:anywhere]">{channelTarget(c)}</div>
        <div className="flex flex-wrap gap-1">
          {c.events.length === 0 ? (
            <Badge tone="warn">No alerts ticked</Badge>
          ) : (
            c.events.map((k) => (
              <Badge key={k} tone="info">
                {titles.get(k) ?? k}
              </Badge>
            ))
          )}
        </div>
        <div className="text-xs">
          <TestStatus c={c} />
        </div>
        <ErrorNote error={test.error ?? remove.error} />
      </div>
      <div className="flex shrink-0 flex-wrap items-center gap-1.5">
        <Button size="sm" onClick={() => test.mutate()} disabled={test.isPending}>
          <Icon name="refresh" className={cx("size-3.5", test.isPending && "animate-spin")} />
          Send test
        </Button>
        <Button size="sm" variant="ghost" onClick={onEdit}>
          <Icon name="pencil" className="size-3.5" />
          Edit
        </Button>
        <ConfirmButton label={`Delete ${c.name}`} confirmLabel="Delete?" onConfirm={() => remove.mutate()} disabled={remove.isPending}>
          Delete
        </ConfirmButton>
      </div>
    </li>
  );
}

type Secrets = { password: string; api_key: string; webhook_url: string };

/** The one secret each kind takes. A webhook's is made by Byname. */
const secretOf: Record<ChannelKind, keyof Secrets | null> = { smtp: "password", email_api: "api_key", slack_webhook: "webhook_url", webhook: null };

/** Adds a channel, or edits one. A secret left blank on an edit stays as it was. */
function ChannelForm({ channel, events, onDone }: { channel?: NotificationChannel; events: EventKind[]; onDone: (c?: NotificationChannel) => void }) {
  const qc = useQueryClient();
  const editing = channel !== undefined;
  const [name, setName] = useState(channel?.name ?? "");
  const [kind, setKind] = useState<ChannelKind>(channel?.kind ?? "smtp");
  const s = channel?.settings ?? {};
  const [host, setHost] = useState(s.host ?? "");
  const [port, setPort] = useState(String(s.port ?? 587));
  const [tls, setTLS] = useState<SMTPTLS>(s.tls ?? "starttls");
  const [username, setUsername] = useState(s.username ?? "");
  const [from, setFrom] = useState(s.from ?? "");
  const [to, setTo] = useState((s.to ?? []).join(", "));
  const [url, setURL] = useState(s.url ?? "");
  const [secrets, setSecrets] = useState<Secrets>({ password: "", api_key: "", webhook_url: "" });
  const [chosen, setChosen] = useState<EventKindName[]>(channel?.events ?? defaultEvents);
  const [enabled, setEnabled] = useState(channel?.enabled ?? true);
  const [created, setCreated] = useState<CreatedChannel | null>(null);

  const input = (): ChannelInput => {
    const recipients = to.split(/[,\n]/).map((x) => x.trim()).filter(Boolean);
    const settings = {
      smtp: { host: host.trim(), port: Number(port), tls, username: username.trim(), from: from.trim(), to: recipients },
      email_api: { from: from.trim(), to: recipients },
      slack_webhook: {},
      webhook: { url: url.trim() },
    }[kind];
    const key = secretOf[kind];
    const secret = key ? secrets[key] : "";
    return { name: name.trim(), kind, settings, events: chosen, enabled, ...(key && secret ? { secrets: { [key]: secret } } : {}) };
  };

  const save = useReauthMutation({
    mutationFn: () => (editing ? api.channels.update(channel.id, input()) : api.channels.create(input())),
    onSuccess: async (c: CreatedChannel) => {
      await qc.invalidateQueries({ queryKey: keys.channels });
      if (c.signing_secret) setCreated(c);
      else onDone(c);
    },
  });

  if (created?.signing_secret) {
    return (
      <DialogBody>
        <div className="space-y-3 text-sm">
          <p>
            <span className="font-medium">“{created.name}” added.</span> <span className="text-muted">Copy its signing secret now. It is shown only once.</span>
          </p>
          <CopyField value={created.signing_secret} />
          <p className="text-xs text-muted">
            Each request carries <code className="font-mono">X-Byname-Signature: t=&lt;unix seconds&gt;,v1=&lt;hex&gt;</code>, an HMAC-SHA256 of{" "}
            <code className="font-mono">t + "." + body</code> keyed with this secret. Check it against the raw body and refuse a timestamp more than a
            few minutes old, so a captured request cannot be replayed.
          </p>
          <div className="flex justify-end">
            <Button variant="primary" onClick={() => onDone(created)}>
              Done
            </Button>
          </div>
        </div>
      </DialogBody>
    );
  }

  const toggle = (k: EventKindName) => setChosen((c) => events.map((e) => e.kind).filter((x) => (x === k ? !c.includes(k) : c.includes(x))));
  const secretHint = editing ? "Saved. Leave blank to keep it." : undefined;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim()) save.mutate();
  };
  const secretField = (key: keyof Secrets, label: string, placeholder: string, hint?: ReactNode) => (
    <Field label={label} hint={hint ?? secretHint}>
      <input
        type="password"
        autoComplete="off"
        className={inputClass}
        placeholder={editing ? "••••••••" : placeholder}
        value={secrets[key]}
        onChange={(e) => setSecrets({ ...secrets, [key]: e.target.value })}
      />
    </Field>
  );

  return (
    <form onSubmit={submit} className="contents">
      <DialogBody>
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Name">
              <input data-autofocus className={inputClass} placeholder="Marketing email" value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label="Kind" hint={editing ? "The kind cannot change." : undefined}>
              <select className={cx(selectClass, "w-full")} value={kind} disabled={editing} onChange={(e) => setKind(e.target.value as ChannelKind)}>
                {kindOrder.map((k) => (
                  <option key={k} value={k}>
                    {kinds[k].label}
                  </option>
                ))}
              </select>
            </Field>
          </div>
          <p className="text-xs text-muted">{kinds[kind].blurb}</p>

          {kind === "smtp" && (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Host">
                <input className={inputClass} placeholder="smtp.example.com" value={host} onChange={(e) => setHost(e.target.value)} />
              </Field>
              <div className="grid grid-cols-[5rem_1fr] gap-3">
                <Field label="Port">
                  <input className={inputClass} inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} />
                </Field>
                <Field label="Encryption">
                  <select className={cx(selectClass, "w-full")} value={tls} onChange={(e) => setTLS(e.target.value as SMTPTLS)}>
                    {tlsModes.map((m) => (
                      <option key={m.value} value={m.value}>
                        {m.label}
                      </option>
                    ))}
                  </select>
                </Field>
              </div>
              <Field label="Username" hint="Leave blank, with the password, for a relay that needs no sign-in.">
                <input className={inputClass} autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
              </Field>
              {secretField("password", "Password", "")}
            </div>
          )}
          {kind === "email_api" && secretField("api_key", "Resend API key", "re_…", editing ? secretHint : "Create one in Resend with sending access only.")}
          {(kind === "smtp" || kind === "email_api") && (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="From">
                <input className={inputClass} placeholder="Byname <alerts@example.com>" value={from} onChange={(e) => setFrom(e.target.value)} />
              </Field>
              <Field label="To" hint="Up to 20 addresses, separated by commas.">
                <input className={inputClass} placeholder="team@example.com" value={to} onChange={(e) => setTo(e.target.value)} />
              </Field>
            </div>
          )}
          {kind === "slack_webhook" &&
            secretField("webhook_url", "Webhook URL", "https://hooks.slack.com/services/…", editing ? secretHint : "In Slack, add the Incoming Webhooks app and copy the URL it gives.")}
          {kind === "webhook" && (
            <Field label="URL" hint={editing ? undefined : "Byname makes a signing secret when you add it and shows it once."}>
              <input className={inputClass} placeholder="https://example.com/hooks/byname" value={url} onChange={(e) => setURL(e.target.value)} />
            </Field>
          )}

          <fieldset>
            <legend className="mb-1.5 text-xs font-medium text-muted">Alerts it gets</legend>
            <div className="space-y-1.5">
              {events.map((e) => (
                <label key={e.kind} className="flex items-start gap-2 text-sm">
                  <input type="checkbox" className="mt-1" checked={chosen.includes(e.kind)} onChange={() => toggle(e.kind)} />
                  <span className="min-w-0">
                    <span className="font-medium">{e.title}</span>
                    <span className="block text-xs text-muted">{e.description}</span>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>
          {editing && (
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
              On. Turned off, it keeps its settings and gets no alerts.
            </label>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => onDone()}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={!name.trim() || save.isPending}>
              {editing ? "Save" : "Add"}
            </Button>
          </div>
          <ErrorNote error={save.error} />
        </div>
      </DialogBody>
    </form>
  );
}

const deliveryTone: Record<Delivery["status"], Tone> = { sent: "success", pending: "warn", failed: "danger" };

function DeliveryLog() {
  const log = useQuery({ queryKey: keys.deliveries, queryFn: () => api.channels.deliveries(), refetchInterval: (q) => (q.state.data?.some((d) => d.status === "pending") ? 5000 : false) });
  return (
    <Card className="mt-10">
      <CardHeader title="Sent alerts" description="Each alert sent to each place, newest first. A failed send is tried again, up to 5 times. Tests show on their place above." />
      {log.isPending ? (
        <Pending />
      ) : log.isError ? (
        <div className="px-4 pb-3">
          <ErrorNote error={log.error} />
        </div>
      ) : log.data.length === 0 ? (
        <p className="border-t border-line px-4 py-6 text-center text-sm text-muted">Nothing sent yet.</p>
      ) : (
        <ul aria-label="Sent alerts" className="divide-y divide-line border-t border-line">
          {log.data.map((d) => (
            <li key={d.id} className="flex flex-col gap-1 px-4 py-2.5 text-sm sm:flex-row sm:items-start sm:gap-4">
              <div className="min-w-0 flex-1">
                <div className="[overflow-wrap:anywhere]">{d.title}</div>
                <div className="text-xs text-muted [overflow-wrap:anywhere]">
                  {d.channel_name} · {kinds[d.channel_kind].label}
                </div>
                {d.error && <div className="mt-0.5 text-xs text-danger [overflow-wrap:anywhere]">{d.error}</div>}
              </div>
              <div className="flex shrink-0 flex-wrap items-center gap-x-2 text-xs text-muted sm:flex-col sm:items-end sm:gap-0.5">
                <Badge tone={deliveryTone[d.status]}>{d.status === "sent" ? "Sent" : d.status === "pending" ? "Retrying" : "Failed"}</Badge>
                <span>
                  {d.attempts} {d.attempts === 1 ? "attempt" : "attempts"} · <RelativeTime iso={d.sent_at ?? d.created_at} />
                  {d.next_attempt_at && (
                    <>
                      , next <RelativeTime iso={d.next_attempt_at} />
                    </>
                  )}
                </span>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

/**
 * Picks which channels get alerts, for onboarding step 6: each channel with
 * an on switch, and Add channel. With none, alerts are in the app only.
 * Admin only, like Settings > Alerts, which manages them later.
 */
export function ChannelPicker() {
  return (
    <ReauthProvider>
      <PickerBody />
    </ReauthProvider>
  );
}

function PickerBody() {
  const qc = useQueryClient();
  const list = useChannels();
  const [adding, setAdding] = useState(false);
  const toggle = useReauthMutation({
    mutationFn: (c: NotificationChannel) => api.channels.update(c.id, { name: c.name, kind: c.kind, settings: c.settings, events: c.events, enabled: !c.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.channels }),
  });
  if (list.isPending) return <Pending />;
  if (list.isError) return <ErrorNote error={list.error} />;
  const { channels, events } = list.data;
  return (
    <div className="space-y-2">
      {channels.length === 0 ? (
        <p className="text-sm text-muted">No channels yet, so alerts show in the app only.</p>
      ) : (
        <ul aria-label="Alert channels" className="space-y-1.5">
          {channels.map((c) => (
            <li key={c.id}>
              <label className="flex items-start gap-2 text-sm">
                <input type="checkbox" className="mt-1" checked={c.enabled} disabled={toggle.isPending} onChange={() => toggle.mutate(c)} />
                <span className="min-w-0">
                  <span className="font-medium [overflow-wrap:anywhere]">{c.name}</span> <span className="text-muted">{kinds[c.kind].label}</span>
                  <span className="block text-xs text-muted [overflow-wrap:anywhere]">{channelTarget(c)}</span>
                </span>
              </label>
            </li>
          ))}
        </ul>
      )}
      <ErrorNote error={toggle.error} />
      <Button size="sm" onClick={() => setAdding(true)}>
        <Icon name="plus" className="size-3.5" />
        Add channel
      </Button>
      <Dialog open={adding} onClose={() => setAdding(false)} title="Add a channel" wide>
        {adding && <ChannelForm events={events} onDone={() => setAdding(false)} />}
      </Dialog>
    </div>
  );
}
