import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { useReauthMutation } from "./Reauth";
import { api, scopeInfo, scopes, type CreatedKey, type Key, type Scope } from "../../api";
import { keys } from "../../queries";
import { Badge, Button, Card, ConfirmButton, CopyField, EmptyState, ErrorNote, Icon, inputClass, Pending, RelativeTime, selectClass } from "../../ui";
import { SettingsHeader } from "./Settings";

/** Lifetimes offered when creating a key, in days; 0 is until revoked. */
const lifetimes = [
  { days: 30, label: "Expires in 30 days" },
  { days: 90, label: "Expires in 90 days" },
  { days: 365, label: "Expires in 1 year" },
  { days: 0, label: "Never expires" },
];

function expiresAt(days: number): string | undefined {
  return days ? new Date(Date.now() + days * 86_400_000).toISOString() : undefined;
}

/** The plaintext key comes back once, to `onCreated`. */
export function CreateKeyForm({ onCreated }: { onCreated: (k: CreatedKey) => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [days, setDays] = useState(90);
  const [chosen, setChosen] = useState<Scope[]>(["results:read"]);
  const toggle = (s: Scope) => setChosen((c) => scopes.filter((x) => (x === s ? !c.includes(s) : c.includes(x))));
  const create = useReauthMutation({
    mutationFn: () => api.keys.create({ name: name.trim(), scopes: chosen, expires_at: expiresAt(days) }),
    onSuccess: (k) => {
      setName("");
      onCreated(k);
      return qc.invalidateQueries({ queryKey: keys.keys });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim() && chosen.length) create.mutate();
  };
  return (
    <form onSubmit={submit} className="space-y-2">
      <div className="flex flex-col gap-2 sm:flex-row">
        <input aria-label="Key name" placeholder="What will use it? Like a CI job or a reporting script" value={name} onChange={(e) => setName(e.target.value)} className={inputClass} />
        <Button type="submit" variant="primary" disabled={!name.trim() || !chosen.length || create.isPending}>
          <Icon name="plus" className="size-3.5" />
          Create key
        </Button>
      </div>
      <fieldset aria-label="Scopes" className="flex flex-wrap gap-x-4 gap-y-1">
        {scopes.map((s) => (
          <label key={s} className="flex items-center gap-1.5 text-sm" title={scopeInfo[s].detail}>
            <input type="checkbox" checked={chosen.includes(s)} onChange={() => toggle(s)} />
            {scopeInfo[s].title} <code className="font-mono text-xs text-muted">{s}</code>
          </label>
        ))}
      </fieldset>
      <div className="flex flex-col gap-2 sm:flex-row">
        <select aria-label="Expiry" className={selectClass} value={days} onChange={(e) => setDays(Number(e.target.value))}>
          {lifetimes.map((l) => (
            <option key={l.days} value={l.days}>
              {l.label}
            </option>
          ))}
        </select>
      </div>
      <ErrorNote error={create.error} />
    </form>
  );
}

export function NewKey({ k, onDone }: { k: CreatedKey; onDone?: () => void }) {
  return (
    <div className="rounded-surface border border-line-strong bg-surface p-3">
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0 text-sm">
          <span className="font-medium [overflow-wrap:anywhere]">“{k.name}” created.</span> <span className="text-muted">Copy the key now. It is shown only once.</span>
        </div>
        {onDone && (
          <Button size="sm" variant="ghost" onClick={onDone} className="-my-1">
            Done
          </Button>
        )}
      </div>
      <CopyField value={k.key} />
    </div>
  );
}

export function ApiKeys() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: keys.keys, queryFn: async () => (await api.keys.list()) ?? [] });
  const [created, setCreated] = useState<CreatedKey | null>(null);
  const revoke = useMutation({
    mutationFn: api.keys.revoke,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.keys }),
  });

  return (
    <>
      <SettingsHeader title="API keys">
        For developers. An API key lets a script read or change Byname over its REST API. Scopes are what a key may do, such as read results or change setup, so tick
        only what it needs. A key never does more than your role allows, and it cannot manage keys, apps or the team. Give each key an expiry unless something you
        cannot easily update depends on it. AI agents such as Claude Code sign in as a person instead, under{" "}
        <Link to="/settings/apps" className="text-fg underline underline-offset-2">
          Apps
        </Link>
        .
      </SettingsHeader>
      <div className="space-y-3">
        <CreateKeyForm onCreated={setCreated} />
        {created && <NewKey k={created} onDone={() => setCreated(null)} />}
      </div>

      <div className="mt-6">
        {list.isPending ? (
          <Pending />
        ) : list.data?.length === 0 ? (
          <EmptyState icon="key" title="No keys yet">
            Create one above for each agent or backend, so you can revoke them one at a time.
          </EmptyState>
        ) : (
          <Card>
            <ul aria-label="API keys" className="divide-y divide-line">
              {list.data?.map((k) => (
                <li key={k.id} className="flex items-center gap-3 px-4 py-3">
                  <span className="grid size-8 shrink-0 place-items-center rounded-control bg-subtle text-muted">
                    <Icon name="key" className="size-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                      <span className="min-w-0 truncate font-medium" title={k.name}>
                        {k.name}
                      </span>
                      <code className="font-mono text-xs text-muted">{k.prefix}…</code>
                      {k.scopes.map((s) => (
                        <Badge key={s} tone="info">
                          {s}
                        </Badge>
                      ))}
                      <KeyExpiry k={k} />
                    </div>
                    <div className="text-xs text-muted">
                      Created <RelativeTime iso={k.created_at} /> · {k.last_used_at ? <>used <RelativeTime iso={k.last_used_at} /></> : "never used"}
                      {k.expires_at && (
                        <>
                          {" "}
                          · {new Date(k.expires_at) <= new Date() ? "expired" : "expires"} <RelativeTime iso={k.expires_at} />
                        </>
                      )}
                    </div>
                  </div>
                  <ConfirmButton label={`Revoke ${k.name}`} confirmLabel="Revoke?" onConfirm={() => revoke.mutate(k.id)} disabled={revoke.isPending}>
                    Revoke
                  </ConfirmButton>
                </li>
              ))}
            </ul>
          </Card>
        )}
      </div>
      <div className="mt-2 empty:hidden">
        <ErrorNote error={list.error ?? revoke.error} />
      </div>
    </>
  );
}

function KeyExpiry({ k }: { k: Key }) {
  if (!k.expires_at) return null;
  const left = new Date(k.expires_at).getTime() - Date.now();
  if (left <= 0) return <Badge tone="danger">Expired</Badge>;
  if (left < 7 * 86_400_000) return <Badge tone="warn">Expires soon</Badge>;
  return null;
}
