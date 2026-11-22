import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type LeftOut, type SocialNetwork, type SocialSettings } from "../api";
import { keys, useLeftOut, useSettings, useSocialSettings } from "../queries";
import { Button, Card, ConfirmButton, cx, ErrorNote, errorMessage, Field, Icon, inputClass, Pending, selectClass, SteadyLabel } from "../ui";
import { NetworkMark, networkOrder, networks } from "../pages/social/model";
import { about, refreshCosts, useCosts } from "./costs";
import { FieldNote, NoteSlot, type Footer } from "./forms";

/**
 * Onboarding's optional social step: what watching social networks does
 * and costs, the switch, which stays off unless turned on, and the people
 * to leave out, starting with the company's own accounts.
 */
export function SocialStep({ footer, onSaved }: { footer: Footer; onSaved: () => void }) {
  const social = useSocialSettings();
  const costs = useCosts();
  const settings = useSettings();
  if (social.isPending || settings.isPending) return <Pending />;
  if (social.isError || settings.isError) return <ErrorNote error={social.error ?? settings.error} />;
  return <Body s={social.data} brand={settings.data.brand.name} tregMonth={costs.data?.social.treg_month_usd} tregNetworks={costs.data?.social.treg_networks} footer={footer} onSaved={onSaved} />;
}

function Body({
  s,
  brand,
  tregMonth,
  tregNetworks,
  footer,
  onSaved,
}: {
  s: SocialSettings;
  brand: string;
  tregMonth?: number;
  tregNetworks?: number;
  footer: Footer;
  onSaved: () => void;
}) {
  const qc = useQueryClient();
  const [on, setOn] = useState(s.enabled);
  const save = useMutation({
    mutationFn: () =>
      api.social.putSettings({
        enabled: on,
        terms: s.custom_terms ? s.terms : [],
        fetch_every_hours: s.fetch_every_hours,
        digest_cadence: s.digest_cadence,
        digest_weekday: s.digest_weekday,
      }),
    onSuccess: (v) => {
      qc.setQueryData(keys.socialSettings, v);
      void qc.invalidateQueries({ queryKey: keys.social });
      void refreshCosts(qc);
      onSaved();
    },
  });
  const name = brand || "your brand";
  const label = on ? "Turn on and continue" : "Continue";

  return (
    <div className="space-y-5">
      <Card className="space-y-3 p-4">
        <p className="text-sm text-muted">
          Byname can also watch X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky for public posts that name {name}, and send you a digest of them.{" "}
          {tregNetworks === 0 ? (
            <>Without a Treg token it searches Bluesky only, which is free.</>
          ) : (
            <>
              At the default settings it costs <span className="font-medium text-fg">{tregMonth != null ? about(tregMonth) : "a little"} a month</span>, billed by Treg. Bluesky
              is free.
            </>
          )}{" "}
          It stays off unless you turn it on.
        </p>
        <label className="flex min-h-11 cursor-pointer items-start gap-2.5 rounded-surface border border-line-strong p-3 text-sm has-checked:border-fg has-checked:bg-subtle">
          <input type="checkbox" className="mt-0.5" checked={on} onChange={(e) => setOn(e.target.checked)} />
          <span className="min-w-0">
            <span className="block font-medium">Watch social networks for posts that name {name}</span>
            <span className="mt-0.5 block text-xs text-muted">It starts once setup is finished, then searches every {s.fetch_every_hours} hours. Change it any time in Settings &gt; Social.</span>
          </span>
        </label>
      </Card>

      {on && <LeaveOut brand={brand} />}

      {footer(
        <Button variant="primary" onClick={() => save.mutate()} disabled={save.isPending}>
          <SteadyLabel labels={["Turn on and continue", "Continue", "Saving…"]} current={save.isPending ? "Saving…" : label} />
        </Button>,
      )}
      <ErrorNote error={save.error} />
    </div>
  );
}

/** People to leave out, so the Social page and the digest show what other people say. */
function LeaveOut({ brand }: { brand: string }) {
  const qc = useQueryClient();
  const list = useLeftOut();
  const remove = useMutation({ mutationFn: api.social.removeLeftOut, onSuccess: () => qc.invalidateQueries({ queryKey: keys.social }) });
  const groups = new Map<string, LeftOut[]>();
  for (const l of list.data ?? []) groups.set(l.label, [...(groups.get(l.label) ?? []), l]);
  return (
    <section aria-labelledby="leave-out-title">
      <h3 id="leave-out-title" className="text-sm font-semibold">
        People to leave out
      </h3>
      <p className="mb-3 text-xs text-muted">
        Start with your company&apos;s own accounts, then your team&apos;s, so you only see what other people say about {brand || "you"}. Byname keeps their posts, just hidden,
        so you can bring them back.
      </p>
      {list.isPending ? (
        <Pending />
      ) : list.isError ? (
        <ErrorNote error={list.error} />
      ) : (
        groups.size > 0 && (
          <ul aria-label="People to leave out" className="mb-3 divide-y divide-line rounded-surface border border-line">
            {[...groups].map(([label, accounts]) => (
              <li key={label} className="px-3 py-2.5">
                <div className="text-sm font-medium [overflow-wrap:anywhere]">{label}</div>
                <ul className="mt-1.5 flex flex-wrap gap-2">
                  {accounts.map((a) => (
                    <li key={a.id} className="flex items-center gap-1.5 rounded-control border border-line py-0.5 pr-1 pl-0.5 text-xs">
                      <NetworkMark network={a.network} className="size-5" />
                      <span className="[overflow-wrap:anywhere]">{a.handle}</span>
                      <ConfirmButton label={`Stop leaving out ${a.handle} on ${networks[a.network].title}`} confirmLabel="Remove?" onConfirm={() => remove.mutate(a.id)} disabled={remove.isPending}>
                        <Icon name="x" className="size-3" />
                      </ConfirmButton>
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        )
      )}
      <ErrorNote error={remove.error} />
      <LeaveOutForm brand={brand} labels={[...groups.keys()]} />
    </section>
  );
}

function LeaveOutForm({ brand, labels }: { brand: string; labels: string[] }) {
  const qc = useQueryClient();
  const [label, setLabel] = useState(labels.length ? "" : "Our accounts");
  const [network, setNetwork] = useState<SocialNetwork>("x");
  const [handle, setHandle] = useState("");
  const [tried, setTried] = useState(false);
  const add = useMutation({
    mutationFn: () => api.social.leaveOut({ network, handle: handle.trim(), label: label.trim() }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: keys.social });
      setHandle("");
      setTried(false);
    },
  });
  const missing = !label.trim() ? "Say who it is, such as Our accounts or a teammate's name." : !handle.trim() ? "Give the handle, or paste a link to the profile." : null;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (!missing) add.mutate();
  };
  const handleHint = network === "youtube" ? "Channel name or link" : network === "bluesky" ? `${brand.toLowerCase().replace(/\s+/g, "") || "name"}.bsky.social` : `@${brand.toLowerCase().replace(/\s+/g, "") || "handle"}`;
  return (
    <form onSubmit={submit} noValidate className="rounded-surface border border-line p-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_9rem_minmax(0,1fr)_auto] sm:items-end">
        <Field label="Who">
          <input className={inputClass} list="leave-out-labels" value={label} maxLength={80} onChange={(e) => setLabel(e.target.value)} placeholder="Our accounts" />
          <datalist id="leave-out-labels">
            {["Our accounts", ...labels.filter((l) => l !== "Our accounts")].map((l) => (
              <option key={l} value={l} />
            ))}
          </datalist>
        </Field>
        <Field label="Network">
          <select className={cx(selectClass, "w-full")} value={network} onChange={(e) => setNetwork(e.target.value as SocialNetwork)}>
            {networkOrder.map((n) => (
              <option key={n} value={n}>
                {networks[n].title}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Handle or profile link">
          <input className={inputClass} value={handle} autoCapitalize="none" spellCheck={false} onChange={(e) => setHandle(e.target.value)} placeholder={handleHint} />
        </Field>
        <Button type="submit" disabled={add.isPending}>
          <Icon name="plus" className="size-3.5" />
          Leave out
        </Button>
      </div>
      <NoteSlot>{tried && missing ? <FieldNote tone="error">{missing}</FieldNote> : add.error ? <FieldNote tone="error">{errorMessage(add.error)}</FieldNote> : null}</NoteSlot>
    </form>
  );
}
