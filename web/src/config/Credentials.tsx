import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Credential, type CredentialKind } from "../api";
import { keys, useIsAdmin, useSurfaces } from "../queries";
import { useReauthMutation } from "../pages/settings/Reauth";
import { Badge, Button, Card, ConfirmButton, cx, ErrorNote, Field, Icon, inputClass, Pending, RelativeTime, SteadyLabel } from "../ui";

/** Every credential, set or not. Admins only: the server refuses everyone else. */
export const useCredentials = () => {
  const admin = useIsAdmin();
  return useQuery({ queryKey: keys.credentials, queryFn: async () => (await api.credentials.list()) ?? [], enabled: admin });
};

const treg = (c: { kind: CredentialKind }) => c.kind === "treg";

export function testTone(status: string) {
  return status === "ok" ? "success" : status ? "danger" : "neutral";
}

/**
 * Pastes a secret. The field is never prefilled and the secret never comes
 * back: after saving it only shows as set. Saving also makes the
 * provider's free test call, so a mistyped key shows at once instead of on
 * the first check.
 */
export function CredentialForm({
  kind,
  title,
  hint,
  set,
  org,
  onDone,
  onCancel,
  compact,
}: {
  kind: CredentialKind;
  title: string;
  hint?: string;
  set: boolean;
  org?: string;
  onDone?: () => void;
  onCancel?: () => void;
  compact?: boolean;
}) {
  const qc = useQueryClient();
  const [secret, setSecret] = useState("");
  const [orgValue, setOrgValue] = useState(org ?? "");
  const save = useReauthMutation({
    mutationFn: async () => {
      await api.credentials.set(kind, secret.trim(), treg({ kind }) ? orgValue.trim() || undefined : undefined);
      return api.credentials.test(kind);
    },
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
          <div className={compact ? "sm:w-48" : undefined}>
            <Field label="Treg organization">
              <input className={inputClass} value={orgValue} onChange={(e) => setOrgValue(e.target.value)} autoCapitalize="none" spellCheck={false} placeholder="Only for a personal token" />
            </Field>
          </div>
        )}
        <div className="flex gap-2">
          {(onCancel ?? (!compact && onDone)) && (
            <Button variant="ghost" onClick={onCancel ?? onDone}>
              Cancel
            </Button>
          )}
          <Button type="submit" variant="primary" disabled={!secret.trim() || save.isPending}>
            <SteadyLabel labels={["Save", "Replace", "Checking…"]} current={save.isPending ? "Checking…" : set ? "Replace" : "Save"} />
          </Button>
        </div>
      </div>
      {treg({ kind }) && (
        <p className="text-xs text-muted">
          A team token works on its own. A personal token belongs to you and can reach several Treg organizations, so give the organization&apos;s name too.
        </p>
      )}
      <ErrorNote error={save.error} />
    </form>
  );
}

/** Settings > Provider keys: every key but Treg's, which has its own page, as rows. */
export function ProviderKeyList() {
  const list = useCredentials();
  if (list.isPending) return <Pending />;
  if (list.isError) return <ErrorNote error={list.error} />;
  return (
    <Card>
      <ul aria-label="Provider keys" className="divide-y divide-line">
        {list.data.filter((c) => !treg(c)).map((c) => (
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
            {c.last_test_status && !c.last_test_status.startsWith("untested") && (
              <Badge tone={testTone(c.last_test_status)} title={c.last_test_status}>
                {c.last_test_status === "ok" ? "Test passed" : "Test failed"}
              </Badge>
            )}
          </div>
          <p className="mt-0.5 text-xs text-muted [overflow-wrap:anywhere]">{c.hint}</p>
          <p className="mt-0.5 text-xs text-muted">
            {c.used_by.length ? (
              <>Used by {c.used_by.map((u) => `${u.surfaces.map((k) => titles.get(k) ?? k).join(", ")} in ${u.entity}`).join("; ")}</>
            ) : (
              "No AI app uses it"
            )}
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
          {c.last_test_status && c.last_test_status !== "ok" && (
            <p className={cx("mt-1 text-xs [overflow-wrap:anywhere]", c.last_test_status.startsWith("untested") ? "text-muted" : "text-danger")}>{c.last_test_status}</p>
          )}
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-1">
          {c.set && (
            <Button size="sm" variant="ghost" onClick={() => test.mutate()} disabled={test.isPending}>
              <SteadyLabel labels={["Test", "Testing…"]} current={test.isPending ? "Testing…" : "Test"} />
            </Button>
          )}
          <Button size="sm" aria-expanded={editing} onClick={() => setEditing(!editing)}>
            {c.set ? "Replace" : "Set"}
          </Button>
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
