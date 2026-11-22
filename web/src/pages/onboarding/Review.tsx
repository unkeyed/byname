import type { ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router";
import { api, type EstimateRow, type StepKey, type Surface } from "../../api";
import { fidelityInfo } from "../../lines";
import { money } from "../../money";
import { keys, useBrands, useCosts, useGsc, useOnboarding, useQuestions, useSettings, useSocialSettings, useSurfaces } from "../../queries";
import { Badge, Button, buttonClass, Card, CardHeader, ErrorNote, Icon, Pending, RelativeTime, ScrollX, SteadyLabel, useLoading } from "../../ui";
import { nextCheckPhrase } from "../../checks";
import { Disclosure } from "../../config/forms";
import { FidelityBadge, routeInfo } from "../../config/WherePeopleAsk";
import type { StepNav } from "./Onboarding";

function Section({ title, step, children }: { title: string; step: StepKey; children: ReactNode }) {
  return (
    <div className="flex items-start gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <h3 className="text-sm font-medium">{title}</h3>
        <div className="mt-0.5 text-sm text-muted [overflow-wrap:anywhere]">{children}</div>
      </div>
      <Link to={`/onboarding/${step}`} className={buttonClass({ size: "sm", variant: "ghost" })} aria-label={`Edit ${title.toLowerCase()}`}>
        <Icon name="pencil" className="size-3.5" />
        Edit
      </Link>
    </div>
  );
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/** Onboarding's last step: everything set, what it costs a check and a month, when results come, and Start first check. */
export function Review({ nav }: { nav: StepNav }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const onboarding = useOnboarding();
  const settings = useSettings();
  const brands = useBrands();
  const questions = useQuestions();
  const surfaces = useSurfaces();
  const social = useSocialSettings();
  const costs = useCosts();
  const gsc = useGsc();
  useLoading(gsc.isPending);
  const start = useMutation({
    mutationFn: async () => {
      await api.onboarding.finish();
      return api.runs.runNow();
    },
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: keys.status }),
        qc.invalidateQueries({ queryKey: keys.onboarding }),
        qc.invalidateQueries({ queryKey: keys.settings }),
        qc.invalidateQueries({ queryKey: keys.runs }),
        qc.invalidateQueries({ queryKey: keys.costs }),
      ]);
      void navigate("/");
    },
    onError: () => qc.invalidateQueries({ queryKey: keys.onboarding }),
  });

  if (settings.isPending || brands.isPending || questions.isPending || surfaces.isPending || social.isPending || costs.isPending) return <Pending />;
  const error = settings.error ?? brands.error ?? questions.error ?? surfaces.error ?? social.error ?? costs.error;
  if (error || !settings.data || !brands.data || questions.data === undefined || !surfaces.data || !social.data || !costs.data) return <ErrorNote error={error} />;

  const s = settings.data;
  const c = costs.data;
  const others = brands.data.filter((b) => b.kind !== "self");
  const competitors = others.filter((b) => b.kind === "competitor");
  const technologies = others.filter((b) => b.kind === "technology");
  const set = questions.data;
  const groups = new Set(set?.questions.map((q) => q.group));
  const leftOut = set?.questions.filter((q) => !q.count_in_scores).length ?? 0;
  const enabled = surfaces.data.filter((x) => x.enabled);
  const pairs = new Map(surfaces.data.flatMap((x) => x.pairs.map((p) => [p.id, p] as const)));
  const byKey = new Map(surfaces.data.map((x) => [x.key, x]));
  const missing = onboarding.data?.missing ?? [];
  const next = onboarding.data?.first_scheduled_check ?? null;
  const est = c.checks.check;
  const socialMonth = c.social.month_usd;
  const cap = s.spend_cap_month_usd ?? c.default_month_cap_usd;
  const gscProperty = gsc.data?.connected ? gsc.data.property : "";

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader title="What it costs" description="Treg bills you for each answer, not Byname. These are estimates." />
        <div className="space-y-3 border-t border-line px-4 py-3 text-sm">
          <p className="text-base font-semibold tabular-nums first-letter:uppercase">
            {money(est.known_usd, "about")} a check, {money(c.checks.month_usd, "about")} a month
          </p>
          <p className="text-muted">
            Each check asks {plural(est.questions, "question")} in {plural(est.pairs, "AI app")}
            {est.pairs !== enabled.length && " and model"}, {plural(est.calls, "answer")} in all. At every {s.check_every_days === 1 ? "day" : `${s.check_every_days} days`} that is{" "}
            {money(c.checks.month_usd, "about")} a month
            {socialMonth > 0 ? <>, plus {money(socialMonth, "about")} a month for social mentions.</> : "."}
            {est.unknown_pairs > 0 && ` ${plural(est.unknown_pairs, "AI app")} ${est.unknown_pairs === 1 ? "has" : "have"} no price yet, so the first check will tell.`}
          </p>
          {est.find_brands && (
            <p className="text-muted">
              Each check also reads its answers for every brand they name, so you can see competitors you didn&apos;t know about: {money(est.find_brands.cost_usd, "about")} a check, included above.
              Logos for those brands cost {money(c.other_brands.logos.cost_per_brand_usd, "about")} each, once, at most {c.other_brands.logos.month_limit} new brands a month. Both can be turned off in Settings &gt; Competitors.
            </p>
          )}
          <p className="text-muted">
            {s.spend_cap_month_usd == null ? (
              <>
                Finishing sets a monthly spend cap of <span className="font-medium text-fg">{money(cap)}</span>, so a mistake can&apos;t run up a bill. You can change it any time in
                Settings.
              </>
            ) : (
              <>
                Your monthly spend cap is <span className="font-medium text-fg">{money(cap)}</span>.
              </>
            )}{" "}
            A single check stops at {money(s.spend_cap_run_usd)}.
          </p>
        </div>
        {est.rows.length > 0 && (
          <div className="border-t border-line p-3">
            <Disclosure summary="Cost per AI app">
              <PairsTable rows={est.rows} pairs={pairs} surfaces={byKey} checksPerMonth={c.checks.checks_per_month} />
            </Disclosure>
          </div>
        )}
      </Card>

      <Card>
        <div className="divide-y divide-line">
          <Section title="Brand and domain" step="brand">
            {s.brand.name || "Not set"} · {s.brand.domains.join(", ") || "no domain"}
            {s.brand.aliases.length > 0 && <> · also {s.brand.aliases.join(", ")}</>}
            <br />
            {s.match_mode === "whole_word" ? "Whole word, case-sensitive" : "Also anywhere, ignoring case"} · {s.market} · {s.language} · {s.time_zone.replaceAll("_", " ")}
          </Section>
          <Section title="Competitors" step="competitors">
            {others.length === 0
              ? "None yet. You can add them later in Settings."
              : `${plural(competitors.length, "competitor")}${technologies.length ? `, ${plural(technologies.length, "technology", "technologies")}` : ""}: ${others.map((b) => b.name).join(", ")}`}
          </Section>
          <Section title="Questions" step="questions">
            {set ? `${plural(set.questions.length, "question")} in ${plural(groups.size, "group")}` : "None yet"}
            {leftOut > 0 && `. ${leftOut} ${leftOut === 1 ? "names" : "name"} ${s.brand.name || "you"} and ${leftOut === 1 ? "is" : "are"} left out of scores.`}
          </Section>
          <Section title="AI apps" step="surfaces">
            {enabled.length === 0 ? "None ticked" : enabled.map((x) => (x.pairs.some((p) => p.model) ? `${x.title} (${x.pairs.filter((p) => p.enabled).map((p) => p.model).join(", ")})` : x.title)).join(", ")}
          </Section>
          <Section title="Search Console" step="search_console">
            {gscProperty ? `Connected: ${gscProperty}` : "Not connected. You can connect it later in Settings."}
          </Section>
          <Section title="Social mentions" step="social">
            {social.data.enabled ? `On, from when setup is finished, for ${money(socialMonth, "about")} a month.` : "Off. You can turn it on later in Settings."}
          </Section>
          <Section title="Schedule and spend caps" step="schedule">
            Every {plural(s.check_every_days, "day")} at {s.check_time} · {money(s.spend_cap_run_usd)} cap per check · {money(cap)} a month
          </Section>
        </div>
      </Card>

      {missing.length > 0 && (
        <div role="status" className="rounded-surface border border-line-strong bg-warn-soft px-4 py-3 text-sm">
          <p className="font-medium">Before the first check:</p>
          <ul className="mt-1 list-disc space-y-0.5 pl-5">
            {missing.map((m) => (
              <li key={m}>{m}</li>
            ))}
          </ul>
        </div>
      )}

      <div className="space-y-2 border-t border-line pt-4">
        <p className="text-sm text-muted">
          Your first results appear in a few minutes.
          {next && (
            <>
              {" "}
              The second check runs {nextCheckPhrase(next)}, so you can see what moves.
            </>
          )}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          {nav.back && (
            <Link to={nav.back} className={buttonClass({ variant: "ghost" })}>
              <Icon name="left" className="size-3.5" />
              Back
            </Link>
          )}
          <span className="ml-auto" />
          <Button variant="primary" onClick={() => start.mutate()} disabled={start.isPending || missing.length > 0}>
            <SteadyLabel labels={["Start first check", "Starting…"]} current={start.isPending ? "Starting…" : "Start first check"} />
          </Button>
        </div>
        <ErrorNote error={start.error} />
      </div>
    </div>
  );
}

function PairsTable({
  rows,
  pairs,
  surfaces,
  checksPerMonth,
}: {
  rows: EstimateRow[];
  pairs: Map<number, { test_status: string; last_tested_at: string | null }>;
  surfaces: Map<string, Surface>;
  checksPerMonth: number;
}) {
  const th = "px-3 py-2 text-left text-xs font-medium whitespace-nowrap text-muted";
  const td = "px-3 py-2 align-top";
  return (
    <ScrollX label="Cost per AI app">
      <table className="w-full min-w-[40rem] text-sm">
        <thead className="bg-header">
          <tr>
            <th className={th}>AI app</th>
            <th className={th}>Model</th>
            <th className={th}>Asked through</th>
            <th className={th}>Accuracy</th>
            <th className={th}>Test</th>
            <th className={`${th} text-right`}>Cost</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {rows.map((r) => {
            const p = pairs.get(r.pair_id);
            const provider = surfaces.get(r.surface)?.routes.find((o) => o.route === r.route)?.provider;
            return (
              <tr key={r.pair_id}>
                <td className={`${td} font-medium`}>{r.title}</td>
                <td className={`${td} font-mono text-xs whitespace-nowrap`}>{r.model || <span className="font-sans text-muted">The app picks</span>}</td>
                <td className={td}>
                  {routeInfo[r.route].title}
                  {r.route !== "treg" && provider && <span className="text-muted"> · {provider}</span>}
                </td>
                <td className={td}>
                  <FidelityBadge fidelity={r.fidelity} />
                  <span className="sr-only">{fidelityInfo[r.fidelity].detail}</span>
                </td>
                <td className={`${td} text-xs whitespace-nowrap`}>
                  {!p || !p.test_status ? (
                    <Badge>Not tested</Badge>
                  ) : p.test_status === "ok" ? (
                    <span className="text-success">
                      Passed{p.last_tested_at && <RelativeTime iso={p.last_tested_at} className="block text-muted" />}
                    </span>
                  ) : (
                    <span className="text-danger">Failed</span>
                  )}
                </td>
                <td className={`${td} text-right whitespace-nowrap tabular-nums`}>
                  {r.cost_usd != null ? (
                    <>
                      {money(r.cost_usd)} a check
                      <span className="block text-xs text-muted">{money(r.cost_usd * checksPerMonth)} a month</span>
                    </>
                  ) : (
                    <span className="text-muted">No price yet</span>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </ScrollX>
  );
}
