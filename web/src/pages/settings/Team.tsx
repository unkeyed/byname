import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useReauthMutation } from "./Reauth";
import { useNavigate } from "react-router";
import { api, inviteURL, roleInfo, roles, type Email, type Role } from "../../api";
import { keys, useAuthOptions, useMe } from "../../queries";
import { Avatar, Badge, Button, Card, ConfirmButton, CopyField, cx, ErrorNote, inputClass, Pending, RelativeTime, selectClass } from "../../ui";
import { SettingsHeader, SettingsSubhead } from "./Settings";

/** An invite made on this page. The API cannot list invites, so only these are shown. */
interface SentInvite {
  email: Email;
  role: Role;
  url: string;
}

export function Team() {
  const [sent, setSent] = useState<SentInvite[]>([]);
  return (
    <>
      <SettingsHeader title="Team" />
      <Members />

      <SettingsSubhead className="mt-10">Invite a teammate</SettingsSubhead>
      <Invite onSent={(i) => setSent([i, ...sent])} />
      {sent.length > 0 && (
        <Card className="mt-3">
          <ul aria-label="Pending invites" className="divide-y divide-line">
            {sent.map((i) => (
              <li key={i.url} className="space-y-2 px-4 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="min-w-0 font-medium [overflow-wrap:anywhere]">{i.email}</span>
                  <Badge tone="info">Invite pending</Badge>
                  <Badge>{i.role}</Badge>
                </div>
                <CopyField value={i.url} label="Copy link" />
                <p className="text-xs text-muted">Send this link to them. It works once and expires in 7 days. It is not shown again after you leave this page.</p>
              </li>
            ))}
          </ul>
        </Card>
      )}

      <SettingsSubhead className="mt-10">Sign-in methods</SettingsSubhead>
      <SignInMethods />
    </>
  );
}

function Members() {
  const me = useMe();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const users = useQuery({ queryKey: keys.users, queryFn: async () => (await api.users.list()) ?? [] });
  const setRole = useReauthMutation({
    mutationFn: ({ id, role }: { id: number; role: Role }) => api.users.setRole(id, role),
    onSuccess: (_, { id }) => qc.invalidateQueries(me.data?.user_id === id ? undefined : { queryKey: keys.users }),
  });
  const remove = useReauthMutation({
    mutationFn: api.users.remove,
    onSuccess: (_, id) => {
      if (me.data?.user_id === id) {
        qc.clear();
        void navigate("/login", { replace: true });
        return;
      }
      return qc.invalidateQueries({ queryKey: keys.users });
    },
  });
  if (users.isPending) return <Pending />;
  if (users.isError) return <ErrorNote error={users.error} />;
  return (
    <>
      <Card>
        <ul aria-label="Team members" className="divide-y divide-line">
          {users.data.map((u) => (
            <li key={u.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <Avatar label={u.name || u.email} size={32} />
              <div className="min-w-0 flex-1 basis-40">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="min-w-0 truncate font-medium" title={u.name || u.email}>
                    {u.name || u.email}
                  </span>
                  {me.data?.user_id === u.id && <Badge>You</Badge>}
                </div>
                {u.name && (
                  <div className="truncate text-xs text-muted" title={u.email}>
                    {u.email}
                  </div>
                )}
              </div>
              <div className="ml-auto flex shrink-0 items-center gap-2">
                <span className="hidden shrink-0 text-xs text-muted sm:inline">
                  Joined <RelativeTime iso={u.created_at} />
                </span>
                <select
                  aria-label={`Role of ${u.name || u.email}`}
                  className={selectClass}
                  value={u.role}
                  disabled={setRole.isPending}
                  onChange={(e) => setRole.mutate({ id: u.id, role: e.target.value as Role })}
                >
                  {roles.map((r) => (
                    <option key={r} value={r} title={roleInfo[r]}>
                      {r}
                    </option>
                  ))}
                </select>
                <ConfirmButton
                  label={`Remove ${u.name || u.email}`}
                  confirmLabel={me.data?.user_id === u.id ? "Remove you and sign out?" : "Remove?"}
                  onConfirm={() => remove.mutate(u.id)}
                  disabled={remove.isPending}
                >
                  Remove
                </ConfirmButton>
              </div>
            </li>
          ))}
        </ul>
      </Card>
      <div className="mt-2 empty:hidden">
        <ErrorNote error={setRole.error ?? remove.error} />
      </div>
    </>
  );
}

function SignInMethods() {
  const options = useAuthOptions();
  if (options.isPending) return <Pending />;
  if (options.isError) return <ErrorNote error={options.error} />;
  const { providers, password_login } = options.data;
  return (
    <Card>
      <ul aria-label="Sign-in methods" className="divide-y divide-line">
        <li className="flex items-center gap-3 px-4 py-3">
          <span className="min-w-0 flex-1 font-medium">Passkeys</span>
          <Badge tone="success">On</Badge>
        </li>
        {providers.map((p) => (
          <li key={p.id} className="space-y-2 px-4 py-3">
            <div className="flex items-center gap-3">
              <span className="min-w-0 flex-1 font-medium">{p.name}</span>
              <Badge tone="success">On</Badge>
            </div>
            <CopyField value={p.callback_url} label="Copy callback URL" />
            <p className="text-xs text-muted">Register this callback URL at {p.name}.</p>
          </li>
        ))}
        <li className="flex items-center gap-3 px-4 py-3">
          <span className="min-w-0 flex-1 font-medium">Password</span>
          {password_login ? <Badge tone="success">On, with a second step</Badge> : <Badge>Off</Badge>}
        </li>
      </ul>
    </Card>
  );
}


const roleMeaning: Record<Role, string> = {
  viewer: "A viewer reads results.",
  editor: "An editor also starts checks and changes setup.",
  admin: "An admin also manages the team, API keys and apps.",
};
function Invite({ onSent }: { onSent: (i: SentInvite) => void }) {
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const invite = useReauthMutation({
    mutationFn: (to: string) => api.invites.create(to, role),
    onSuccess: ({ token }, to) => {
      onSent({ email: to, role, url: inviteURL(token) });
      setEmail("");
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (email.trim()) invite.mutate(email.trim());
  };
  return (
    <div className="space-y-2">
      <form onSubmit={submit} className="flex flex-col gap-2 sm:flex-row">
        <input type="email" required aria-label="Teammate's email" placeholder="teammate@company.com" value={email} onChange={(e) => setEmail(e.target.value)} className={inputClass} />
        <select aria-label="Role" className={cx(selectClass, "sm:w-28 sm:shrink-0")} value={role} onChange={(e) => setRole(e.target.value as Role)}>
          {roles.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
        <Button type="submit" variant="primary" disabled={invite.isPending}>
          Create invite link
        </Button>
      </form>
      <p className="text-xs text-muted">{roleMeaning[role]}</p>
      <ErrorNote error={invite.error} />
    </div>
  );
}
