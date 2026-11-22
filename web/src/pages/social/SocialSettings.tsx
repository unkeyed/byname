import { useEffect, useState, type FormEvent } from "react";
import { Link, useLocation } from "react-router";
import type { Costs, SocialSettings as Settings } from "../../api";
import { ReadOnlyNote, SubmitButton } from "../../config/forms";
import { money } from "../../money";
import { useCan, useCosts, useIsAdmin, useLeftOut, useSocialSettings } from "../../queries";
import { Badge, Button, cx, EmptyState, ErrorNote, Field, Icon, inputClass, Pending, RelativeTime, selectClass } from "../../ui";
import { SettingsHeader, SettingsSubhead } from "../settings/Settings";
import { LeftOutEditor } from "./LeftOut";
import { NetworkMark, socialCost, tregPath, useSaveSocial, weekdays } from "./model";

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
              {on ? "On." : "Off until you turn it on."}{costs && ` ${socialCost(s, costs)}`}
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
                    {n.route === "direct" ? (
                      <Badge tone="success">Free, no token</Badge>
                    ) : (
                      <Badge tone="outline">{costs ? `${money(n.cost_per_call_usd * costs.terms * costs.fetches_per_month, "about")} a month through Treg` : "Through Treg"}</Badge>
                    )}
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
          The digest lists the new mentions. It shows under the bell when each {cadence === "weekly" ? "week" : "day"} ends in {s.time_zone}, and goes wherever notifications go that get
          the Social digest, chosen in{" "}
          <Link to="/settings/notifications" className="text-fg underline underline-offset-2">
            Settings, Notifications
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
  return (
    <section id="left-out" className="scroll-mt-4">
      <SettingsSubhead>People left out</SettingsSubhead>
      <p className="mb-3 text-sm text-muted">
        Posts by these accounts are hidden from the Social page and the digest, past posts included. Byname keeps them, so taking an account off the list brings its posts back.
      </p>
      <LeftOutEditor
        canEdit={canEdit}
        empty={
          <EmptyState icon="users" title="Nobody is left out">
            {canEdit ? "Your team's own posts are included. Add your teammates' handles and your company's own accounts so you see what other people say." : "An admin or editor can add accounts here."}
          </EmptyState>
        }
      />
    </section>
  );
}
