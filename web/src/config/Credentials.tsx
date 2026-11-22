import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Credential, type CredentialKind } from "../api";
import { keys, useIsAdmin, useSurfaces } from "../queries";
import { useReauthMutation } from "../pages/settings/Reauth";
import { Badge, Button, Card, ConfirmButton, ErrorNote, Field, Icon, inputClass, Pending, RelativeTime } from "../ui";

/** Every credential, set or not. Admins only: the server refuses everyone else. */
export const useCredentials = () => {
  const admin = useIsAdmin();
  return useQuery({ queryKey: keys.credentials, queryFn: async () => (await api.credentials.list()) ?? [], enabled: admin });
};

const treg = (c: { kind: CredentialKind }) => c.kind === "treg";

/** Treg first, then the rest in the server's order. */
const ordered = (list: Credential[]) => [...list.filter(treg), ...list.filter((c) => !treg(c))];

export function testTone(status: string) {
  return status === "ok" ? "success" : status ? "danger" : "neutral";
}

/**
 * Pastes a secret. The field is never prefilled and the secret never comes
 * back: after saving it only shows as set.
 */
export function CredentialForm({ kind, title, hint, set, org, onDone, compact }: { kind: CredentialKind; title: string; hint?: string; set: boolean; org?: string; onDone?: () => void; compact?: boolean }) {
  const qc = useQueryClient();
  const [secret, setSecret] = useState("");
  const [orgValue, setOrgValue] = useState(org ?? "");
  const save = useReauthMutation({
    mutationFn: () => api.credentials.set(kind, secret.trim(), treg({ kind }) ? orgValue.trim() || undefined : undefined),
    onSuccess: async () => {
      setSecret("");
      await Promise.all([qc.invalidateQueries({ queryKey: keys.credentials }), qc.invalidateQueries({ queryKey: ["models"] })]);
      onDone?.();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (secret.trim()) save.mutate();
  };
  return (
    <form onSubmit={submit} className="space-y-2">
      <div className={compact ? "flex flex-col gap-2 sm:flex-row sm:items-end" : "space-y-2"}>
        <div className="min-w-0 flex-1">
          <Field label={`${title} ${kind === "treg" ? "token" : "key"}`} hint={hint}>
            <input
              type="password"
              className={inputClass}
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              autoComplete="off"
              spellCheck={false}
              placeholder={set ? "Paste a new one to replace it" : "Paste it here"}
            />
          </Field>
        </div>
        {treg({ kind }) && (
          <div className={compact ? "sm:w-44" : undefined}>
            <Field label="Org (optional)" hint={compact ? undefined : "Only for a personal token that belongs to several orgs."}>
              <input className={inputClass} value={orgValue} onChange={(e) => setOrgValue(e.target.value)} autoCapitalize="none" spellCheck={false} placeholder="northwind" />
            </Field>
          </div>
        )}
        <div className="flex gap-2">
          {onDone && !compact && (
            <Button variant="ghost" onClick={onDone}>
              Cancel
            </Button>
          )}
          <Button type="submit" variant="primary" disabled={!secret.trim() || save.isPending}>
            {save.isPending ? "Saving…" : set ? "Replace" : "Save"}
          </Button>
        </div>
      </div>
      <ErrorNote error={save.error} />
    </form>
  );
}

/** Settings > Credentials: the Treg token and every provider key, as rows. */
export function Credentials() {
  const list = useCredentials();
  if (list.isPending) return <Pending />;
  if (list.isError) return <ErrorNote error={list.error} />;
  return (
    <Card>
      <ul aria-label="Credentials" className="divide-y divide-line">
        {ordered(list.data).map((c) => (
          <CredentialRow key={c.kind} c={c} />
        ))}
      </ul>
    </Card>
  );
}

function CredentialRow({ c }: { c: Credential }) {
  const qc = useQueryClient();
  const surfaces = useSurfaces();
  const [editing, setEditing] = useState(false);
  const titles = new Map((surfaces.data ?? []).map((s) => [s.key, s.title]));
  const test = useMutation({ mutationFn: () => api.credentials.test(c.kind), onSuccess: () => qc.invalidateQueries({ queryKey: keys.credentials }) });
  const remove = useReauthMutation({ mutationFn: () => api.credentials.remove(c.kind), onSuccess: () => qc.invalidateQueries({ queryKey: keys.credentials }) });
  return (
    <li className="space-y-3 px-4 py-3">
      <div className="flex flex-wrap items-start gap-x-3 gap-y-2">
        <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-control bg-subtle text-muted">
          <Icon name="key" className="size-4" />
        </span>
        <div className="min-w-0 flex-1 basis-56">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{c.title}</span>
            {c.set ? <Badge tone="success">Set</Badge> : <Badge>Not set</Badge>}
            {c.set && c.org && <Badge tone="outline">org {c.org}</Badge>}
            {c.last_test_status && (
              <Badge tone={testTone(c.last_test_status)} title={c.last_test_status}>
                {c.last_test_status === "ok" ? "Test passed" : "Test failed"}
              </Badge>
            )}
          </div>
          <p className="mt-0.5 text-xs text-muted [overflow-wrap:anywhere]">{c.hint}</p>
          <p className="mt-0.5 text-xs text-muted">
            {c.used_by.length ? <>Used by {c.used_by.map((k) => titles.get(k) ?? k).join(", ")}</> : "No surface uses it"}
            {c.updated_at && (
              <>
                {" "}
                · saved <RelativeTime iso={c.updated_at} />
              </>
            )}
            {c.last_tested_at && (
              <>
                {" "}
                · tested <RelativeTime iso={c.last_tested_at} />
              </>
            )}
          </p>
          {c.last_test_status && c.last_test_status !== "ok" && <p className="mt-1 text-xs text-danger [overflow-wrap:anywhere]">{c.last_test_status}</p>}
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-1">
          {c.set && (
            <Button size="sm" variant="ghost" onClick={() => test.mutate()} disabled={test.isPending}>
              {test.isPending ? "Testing…" : "Test"}
            </Button>
          )}
          {!editing && (
            <Button size="sm" onClick={() => setEditing(true)}>
              {c.set ? "Replace" : "Set"}
            </Button>
          )}
          {c.set && (
            <ConfirmButton label={`Remove the ${c.title} credential`} confirmLabel="Remove?" onConfirm={() => remove.mutate()} disabled={remove.isPending}>
              Remove
            </ConfirmButton>
          )}
        </div>
      </div>
      {editing && <CredentialForm kind={c.kind} title={c.title} set={c.set} org={c.org} onDone={() => setEditing(false)} />}
      <ErrorNote error={test.error ?? remove.error} />
    </li>
  );
}
