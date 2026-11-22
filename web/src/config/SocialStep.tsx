import { useState } from "react";
import type { SocialSettings } from "../api";
import { LeftOutEditor } from "../pages/social/LeftOut";
import { useSaveSocial } from "../pages/social/model";
import { useCosts, useSettings, useSocialSettings } from "../queries";
import { Button, Card, ErrorNote, Pending, SteadyLabel } from "../ui";
import { about } from "./costs";
import type { Footer } from "./forms";

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
  const [on, setOn] = useState(s.enabled);
  const save = useSaveSocial(onSaved);
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
        <Button variant="primary" onClick={() =>
            save.mutate({
              enabled: on,
              terms: s.custom_terms ? s.terms : [],
              fetch_every_hours: s.fetch_every_hours,
              digest_cadence: s.digest_cadence,
              digest_weekday: s.digest_weekday,
            })
          } disabled={save.isPending}>
          <SteadyLabel labels={["Turn on and continue", "Continue", "Saving…"]} current={save.isPending ? "Saving…" : label} />
        </Button>,
      )}
      <ErrorNote error={save.error} />
    </div>
  );
}

/** People to leave out, so the Social page and the digest show what other people say. */
function LeaveOut({ brand }: { brand: string }) {
  return (
    <section aria-labelledby="leave-out-title">
      <h3 id="leave-out-title" className="text-sm font-semibold">
        People to leave out
      </h3>
      <p className="mb-3 text-xs text-muted">
        Start with your company&apos;s own accounts, then your team&apos;s, so you only see what other people say about {brand || "you"}. Byname keeps their posts, just hidden,
        so you can bring them back.
      </p>
      <LeftOutEditor canEdit brand={brand} />
    </section>
  );
}
