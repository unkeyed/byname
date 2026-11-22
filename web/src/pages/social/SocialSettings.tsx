import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useLocation } from "react-router";
import { api, type Costs, type LeftOut, type SocialNetwork, type SocialSettings as Settings } from "../../api";
import { ReadOnlyNote, SubmitButton } from "../../config/forms";
import { keys, useCan, useCosts, useIsAdmin, useLeftOut, useSocialSettings } from "../../queries";
import { Badge, Button, ConfirmButton, cx, EmptyState, ErrorNote, Field, Icon, inputClass, labelClass, Pending, RelativeTime, selectClass } from "../../ui";
import { SettingsHeader, SettingsSubhead } from "../settings/Settings";
import { NetworkMark, networkOrder, networks, socialCost, tregPath, useSaveSocial, weekdays } from "./model";

const maxTerms = 5;
const fetchEvery = [1, 3, 6, 12, 24];

export function SocialSettings() {
  const settings = useSocialSettings();
  const costs = useCosts();
  const leftOut = useLeftOut();
  const canEdit = useCan("config:write");
  const { hash } = useLocation();
  const linked = hash === "#left-out" && !settings.isPending && !costs.isPending && !leftOut.isPending;
  useEffect(() => {
    if (linked) document.getElementById("left-out")?.scrollIntoView({ block: "start" });
  }, [linked]);
  return (
    <>
      <SettingsHeader title="Social">
        Byname can watch social networks for mentions, posts that name you, and send you a digest of the new ones. Leave out your team and your company&apos;s own accounts
        so the Social page and the digest show what other people say.
      </SettingsHeader>
      {!canEdit && <ReadOnlyNote />}
      <div className="space-y-10">
        {settings.isPending || costs.isPending ? (
          <Pending />
        ) : settings.isError ? (
          <ErrorNote error={settings.error} />
        ) : (
          <SearchForm key={JSON.stringify(settings.data)} s={settings.data} costs={costs.data?.social} canEdit={canEdit} />
        )}
        <LeftOutSection canEdit={canEdit} />
      </div>
    </>
  );
}

function SearchForm({ s, costs, canEdit }: { s: Settings; costs: Costs["social"] | undefined; canEdit: boolean }) {
  const admin = useIsAdmin();
  const [on, setOn] = useState(s.enabled);
  const [terms, setTerms] = useState<string[]>(s.custom_terms ? s.terms : []);
  const [enabled, setEnabled] = useState<Record<string, boolean>>(Object.fromEntries(s.networks.map((n) => [n.network, n.enabled])));
  const [every, setEvery] = useState(s.fetch_every_hours);
  const [cadence, setCadence] = useState(s.digest_cadence);
  const [weekday, setWeekday] = useState(s.digest_weekday);
  const [saved, setSaved] = useState(false);
  const save = useSaveSocial(() => setSaved(true));
  const cleaned = terms.map((t) => t.trim()).filter(Boolean);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setSaved(false);
    save.mutate({
      enabled: on,
      terms: cleaned,
      fetch_every_hours: every,
      digest_cadence: cadence,
      digest_weekday: weekday,
      networks: s.networks.map((n) => ({ network: n.network, enabled: enabled[n.network] })),
    });
  };
  const defaults = s.default_terms.map((t) => `"${t}"`).join(" and ");

  return (
    <form onSubmit={submit} className="space-y-10">
      <section>
        <label className="flex items-start gap-3 text-sm">
          <input type="checkbox" className="mt-1 size-3.5 accent-[var(--fg)]" checked={on} disabled={!canEdit} onChange={(e) => setOn(e.target.checked)} />
          <span>
            <span className="font-medium">Watch X, Reddit, Hacker News, LinkedIn, YouTube, Threads and Bluesky for mentions</span>
            <span className="mt-0.5 block text-muted">
              Off until you turn it on.{costs && ` ${socialCost(s, costs)}`}
            </span>
          </span>
        </label>
      </section>

      <section>
        <SettingsSubhead>Search terms</SettingsSubhead>
        <p className="mb-3 text-sm text-muted">
          {defaults ? `Leave these empty to search for ${defaults}. ` : "Set your brand under General, or add terms here. "}
          Use the distinctive form, such as your full product name or domain: a common word on its own matches posts about something else. Up to {maxTerms} terms.
        </p>
        <div className="space-y-2">
          {canEdit && terms.length < maxTerms && (
            <Button size="sm" onClick={() => setTerms([...terms, ""])}>
              <Icon name="plus" className="size-3.5" />
              Add a term
            </Button>
          )}
          {terms.map((t, i) => (
            <div key={i} className="flex gap-2">
              <input
                aria-label={`Search term ${i + 1}`}
                autoFocus={i === terms.length - 1 && t === ""}
                className={inputClass}
                value={t}
                disabled={!canEdit}
                maxLength={100}
                onChange={(e) => setTerms(terms.map((v, j) => (j === i ? e.target.value : v)))}
              />
              {canEdit && (
                <Button variant="ghost" onClick={() => setTerms(terms.filter((_, j) => j !== i))} aria-label={`Remove search term ${i + 1}`} className="w-9 shrink-0 px-0">
                  <Icon name="x" className="size-3.5" />
                </Button>
              )}
            </div>
          ))}
          {terms.length === 0 && (
            <p className="rounded-control border border-dashed border-line-strong px-3 py-2 text-sm text-muted">
              Searching for {defaults || "nothing yet"}.
            </p>
          )}
        </div>
      </section>

      <section>
        <SettingsSubhead>Networks</SettingsSubhead>
        {!s.token_set && (
          <p className="mb-3 text-sm text-muted">
            No Treg token is stored, so only Bluesky is searched.{" "}
            {admin ? (
              <Link to={tregPath} className="text-fg underline underline-offset-2">
                Add the Treg token
              </Link>
            ) : (
              "An admin can add the token in Settings, Treg."
            )}
          </p>
        )}
        <ul className="divide-y divide-line rounded-surface border border-line">
          {s.networks.map((n) => (
            <li key={n.network}>
              <label className="flex items-start gap-3 px-3 py-2.5">
                <input
                  type="checkbox"
                  className="mt-1.5 size-3.5 accent-[var(--fg)]"
                  checked={enabled[n.network]}
                  disabled={!canEdit}
                  onChange={(e) => setEnabled({ ...enabled, [n.network]: e.target.checked })}
                />
                <NetworkMark network={n.network} />
                <span className="min-w-0 flex-1 text-sm">
                  <span className="flex flex-wrap items-center gap-2 font-medium">
                    {n.title}
                    {n.route === "direct" ? <Badge tone="success">Free, no token</Badge> : <Badge tone="outline">${n.cost_per_call_usd} a call through Treg</Badge>}
                    {n.unavailable && <Badge tone="warn">Unavailable</Badge>}
                  </span>
                  <span className="mt-0.5 block text-xs text-muted [overflow-wrap:anywhere]">
                    {n.last_call_at ? (
                      <>
                        Last searched <RelativeTime iso={n.last_call_at} />
                        {n.last_error && <span className={n.unavailable ? "text-warn" : "text-danger"}>. {n.last_error}</span>}
                      </>
                    ) : (
                      "Not searched yet"
                    )}
                  </span>
                </span>
              </label>
            </li>
          ))}
        </ul>
      </section>

      <section>
        <SettingsSubhead>Schedule</SettingsSubhead>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field label="Search every">
            <select className={cx(selectClass, "w-full")} value={every} disabled={!canEdit} onChange={(e) => setEvery(Number(e.target.value))}>
              {fetchEvery.map((h) => (
                <option key={h} value={h}>
                  {h === 1 ? "Hour" : h === 24 ? "Day" : `${h} hours`}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Digest">
            <select className={cx(selectClass, "w-full")} value={cadence} disabled={!canEdit} onChange={(e) => setCadence(e.target.value as Settings["digest_cadence"])}>
              <option value="daily">Daily</option>
              <option value="weekly">Weekly</option>
            </select>
          </Field>
          {cadence === "weekly" && (
            <Field label="Week starts on">
              <select className={cx(selectClass, "w-full")} value={weekday} disabled={!canEdit} onChange={(e) => setWeekday(Number(e.target.value))}>
                {weekdays.map((d, i) => (
                  <option key={d} value={i}>
                    {d}
                  </option>
                ))}
              </select>
            </Field>
          )}
        </div>
        <p className="mt-2 text-xs text-muted">
          The digest lists the new mentions. It shows in Alerts when each {cadence === "weekly" ? "week" : "day"} ends in {s.time_zone}, and goes wherever alerts go that get
          the Social digest, chosen in{" "}
          <Link to="/settings/alerts" className="text-fg underline underline-offset-2">
            Settings, Alerts
          </Link>
          .
        </p>
      </section>

      {canEdit && (
        <div className="space-y-2">
          <div className="flex flex-wrap items-center justify-end gap-2">
            <SubmitButton pending={save.isPending} saved={saved && !save.isPending} />
          </div>
          <ErrorNote error={save.error} />
        </div>
      )}
    </form>
  );
}

function LeftOutSection({ canEdit }: { canEdit: boolean }) {
  const qc = useQueryClient();
  const list = useLeftOut();
  const remove = useMutation({
    mutationFn: api.social.removeLeftOut,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.social }),
  });
  const groups = new Map<string, LeftOut[]>();
  for (const l of list.data ?? []) groups.set(l.label, [...(groups.get(l.label) ?? []), l]);

  return (
    <section id="left-out" className="scroll-mt-4">
      <SettingsSubhead>People left out</SettingsSubhead>
      <p className="mb-3 text-sm text-muted">
        Posts by these accounts are hidden from the Social page and the digest, past posts included. Byname keeps them, so taking an account off the list brings its posts back.
      </p>
      {list.isPending ? (
        <Pending />
      ) : list.isError ? (
        <ErrorNote error={list.error} />
      ) : groups.size === 0 ? (
        <EmptyState icon="users" title="Nobody is left out">
          {canEdit ? "Your team's own posts are included. Add your teammates' handles and your company's own accounts so you see what other people say." : "An admin or editor can add accounts here."}
        </EmptyState>
      ) : (
        <ul aria-label="People left out" className="divide-y divide-line rounded-surface border border-line">
          {[...groups].map(([label, accounts]) => (
            <li key={label} className="px-3 py-2.5">
              <div className="text-sm font-medium [overflow-wrap:anywhere]">{label}</div>
              <ul className="mt-1.5 flex flex-wrap gap-2">
                {accounts.map((a) => (
                  <li key={a.id} className="flex items-center gap-1.5 rounded-control border border-line py-0.5 pr-1 pl-0.5 text-xs">
                    <NetworkMark network={a.network} className="size-5" />
                    <a href={a.profile} target="_blank" rel="noreferrer" className="inline-flex min-h-11 min-w-11 items-center [overflow-wrap:anywhere] hover:underline md:min-h-0 md:min-w-0">
                      {a.handle}
                    </a>
                    <span className="text-faint tabular-nums" title="Stored mentions it hides">
                      {a.mentions}
                    </span>
                    {canEdit && (
                      <ConfirmButton label={`Stop leaving out ${a.handle} on ${networks[a.network].title}`} confirmLabel="Remove?" onConfirm={() => remove.mutate(a.id)} disabled={remove.isPending}>
                        <Icon name="x" className="size-3" />
                      </ConfirmButton>
                    )}
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ul>
      )}
      <ErrorNote error={remove.error} />
      {canEdit && <AddLeftOut labels={[...groups.keys()]} />}
    </section>
  );
}

function AddLeftOut({ labels }: { labels: string[] }) {
  const qc = useQueryClient();
  const [label, setLabel] = useState("");
  const [network, setNetwork] = useState<SocialNetwork>("x");
  const [handle, setHandle] = useState("");
  const add = useMutation({
    mutationFn: () => api.social.leaveOut({ network, handle: handle.trim(), label: label.trim() }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: keys.social });
      setHandle("");
    },
  });
  const valid = label.trim() !== "" && handle.trim() !== "";
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (valid) add.mutate();
  };
  return (
    <form onSubmit={submit} className="mt-4 rounded-surface border border-line p-3">
      <p className={labelClass}>Leave out an account</p>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_9rem_minmax(0,1fr)_auto] sm:items-end">
        <Field label="Who">
          <input className={inputClass} list="left-out-labels" value={label} maxLength={80} onChange={(e) => setLabel(e.target.value)} placeholder="Ada Lindqvist, or Our accounts" />
          <datalist id="left-out-labels">
            {labels.map((l) => (
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
          <input
            className={inputClass}
            value={handle}
            autoCapitalize="none"
            spellCheck={false}
            onChange={(e) => setHandle(e.target.value)}
            placeholder={network === "youtube" ? "Channel name or link" : network === "bluesky" ? "name.bsky.social" : "@handle or a link"}
          />
        </Field>
        <Button type="submit" variant="primary" disabled={!valid || add.isPending}>
          Leave out
        </Button>
      </div>
      <p className="mt-2 text-xs text-muted">Give one person several handles by using the same name. A YouTube channel is matched by its name as YouTube search shows it.</p>
      <ErrorNote error={add.error} />
    </form>
  );
}
