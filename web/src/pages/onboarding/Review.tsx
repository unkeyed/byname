import type { ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router";
import { api, type EstimateRow, type StepKey, type Surface } from "../../api";
import { fidelityInfo, usd } from "../../lines";
import { keys, useBrands, useOnboarding, useQuestions, useSettings, useSurfaces } from "../../queries";
import { Badge, Button, buttonClass, Card, CardHeader, ErrorNote, Icon, PageSpinner, RelativeTime, ScrollX } from "../../ui";
import { FidelityBadge, routeInfo } from "../../config/WherePeopleAsk";
import type { StepNav } from "./Onboarding";

const sourceLabel: Record<EstimateRow["source"], string> = {
  history: "Mean",
  sample: "Nov 21 sample",
  unknown: "until the first check",
};

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

/** Onboarding step 7: everything set, the pairs a check asks, and what a check should cost. */
export function Review({ nav }: { nav: StepNav }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const onboarding = useOnboarding();
  const settings = useSettings();
  const brands = useBrands();
  const questions = useQuestions();
  const surfaces = useSurfaces();
  const estimate = useQuery({ queryKey: keys.estimate, queryFn: api.estimate });
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
      ]);
      void navigate("/");
    },
    onError: () => qc.invalidateQueries({ queryKey: keys.onboarding }),
  });

  if (settings.isPending || brands.isPending || questions.isPending || surfaces.isPending) return <PageSpinner />;
  const error = settings.error ?? brands.error ?? questions.error ?? surfaces.error;
  if (error || !settings.data || !brands.data || questions.data === undefined || !surfaces.data) return <ErrorNote error={error} />;

  const s = settings.data;
  const others = brands.data.filter((b) => b.kind !== "self");
  const competitors = others.filter((b) => b.kind === "competitor");
  const technologies = others.filter((b) => b.kind === "technology");
  const set = questions.data;
  const groups = new Set(set?.questions.map((q) => q.group));
  const enabled = surfaces.data.filter((x) => x.enabled);
  const pairs = new Map(surfaces.data.flatMap((x) => x.pairs.map((p) => [p.id, p] as const)));
  const byKey = new Map(surfaces.data.map((x) => [x.key, x]));
  const missing = onboarding.data?.missing ?? [];

  return (
    <div className="space-y-6">
      <Card>
        <div className="divide-y divide-line">
          <Section title="Brand and domain" step="brand">
            {s.brand.name || "Not set"} · {s.brand.domains.join(", ") || "no domain"}
            {s.brand.aliases.length > 0 && <> · also {s.brand.aliases.join(", ")}</>}
            <br />
            {s.match_mode === "whole_word" ? "Whole word, case-sensitive" : "Also anywhere, ignoring case"} · {s.market} · {s.language} · {s.time_zone.replaceAll("_", " ")}
          </Section>
          <Section title="Competitors" step="competitors">
            {others.length === 0 ? "None yet" : `${plural(competitors.length, "competitor")}, ${plural(technologies.length, "technology", "technologies")}: ${others.map((b) => b.name).join(", ")}`}
          </Section>
          <Section title="Questions" step="questions">
            {set ? `${plural(set.questions.length, "question")} in ${plural(groups.size, "group")}, version ${set.version}` : "None yet"}
          </Section>
          <Section title="Where people ask" step="surfaces">
            {enabled.length === 0 ? "No surface ticked" : enabled.map((x) => (x.pairs.some((p) => p.model) ? `${x.title} (${x.pairs.filter((p) => p.enabled).map((p) => p.model).join(", ")})` : x.title)).join(", ")}
          </Section>
          <Section title="Search Console" step="search_console">
            Not connected. Connect it later in Settings &gt; Search Console.
          </Section>
          <Section title="Schedule and limits" step="schedule">
            Every {plural(s.check_every_days, "day")} at {s.check_time} · {usd(s.spend_cap_run_usd)} cap per check
            {s.spend_cap_month_usd != null ? ` · ${usd(s.spend_cap_month_usd)} a month` : " · no monthly cap"} · {s.concurrency} at once, {plural(s.retries, "retry", "retries")},{" "}
            {s.timeout_s} s timeout
          </Section>
        </div>
      </Card>

      <Card>
        {estimate.isPending ? (
          <PageSpinner />
        ) : estimate.isError ? (
          <div className="p-4">
            <ErrorNote error={estimate.error} />
          </div>
        ) : (
          <>
            <CardHeader
              title={`${estimate.data.calls.toLocaleString()} calls per check`}
              description={`${plural(estimate.data.questions, "question")} × ${plural(estimate.data.pairs, "surface and model pair")}. Each pair is its own series.`}
            >
              <div className="md:text-right">
                <div className="text-base font-semibold tabular-nums">
                  {estimate.data.unknown_pairs > 0 && estimate.data.known_usd > 0 && "at least "}
                  {estimate.data.known_usd > 0 || estimate.data.unknown_pairs === 0 ? usd(estimate.data.known_usd) : "Unknown"}
                </div>
                <div className="text-xs text-muted">estimated per check</div>
              </div>
            </CardHeader>
            <p className="border-t border-line px-4 py-2 text-xs text-muted">
              An estimate from the mean cost of past answers on each endpoint, or the costs sampled on Nov 21, 2026 where there is no history yet. API calls are billed by
              tokens, so real costs move with every answer.
              {estimate.data.unknown_pairs > 0 && ` ${plural(estimate.data.unknown_pairs, "pair")} with neither ${estimate.data.unknown_pairs === 1 ? "is" : "are"} unknown until the first check.`}{" "}
              The spend cap of {usd(s.spend_cap_run_usd)} stops a check that runs over.
            </p>
            {estimate.data.rows.length > 0 && <PairsTable rows={estimate.data.rows} pairs={pairs} surfaces={byKey} />}
          </>
        )}
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
        <ErrorNote error={start.error} />
        <div className="flex flex-wrap items-center gap-2">
          {nav.back && (
            <Link to={nav.back} className={buttonClass({ variant: "ghost" })}>
              <Icon name="left" className="size-3.5" />
              Back
            </Link>
          )}
          <span className="ml-auto" />
          <Button variant="primary" onClick={() => start.mutate()} disabled={start.isPending || missing.length > 0}>
            {start.isPending ? "Starting…" : "Start first check"}
          </Button>
        </div>
      </div>
    </div>
  );
}

function PairsTable({ rows, pairs, surfaces }: { rows: EstimateRow[]; pairs: Map<number, { test_status: string; last_tested_at: string | null }>; surfaces: Map<string, Surface> }) {
  const th = "px-3 py-2 text-left text-xs font-medium whitespace-nowrap text-muted";
  const td = "px-3 py-2 align-top";
  return (
    <ScrollX label="Surface and model pairs">
      <table className="w-full min-w-[40rem] border-t border-line text-sm">
        <thead className="bg-header">
          <tr>
            <th className={th}>Surface</th>
            <th className={th}>Model</th>
            <th className={th}>Route</th>
            <th className={th}>Fidelity</th>
            <th className={th}>Last test</th>
            <th className={`${th} text-right`}>Cost per call</th>
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
                    <span className="text-danger" title={p.test_status}>
                      Failed
                    </span>
                  )}
                </td>
                <td className={`${td} text-right`}>
                  <span className="tabular-nums">{r.cost_per_call_usd != null ? usd(r.cost_per_call_usd) : <span className="text-muted">Unknown</span>}</span>
                  <span className="block text-xs text-muted">
                    {sourceLabel[r.source]}
                    {r.source === "history" && r.samples > 0 && ` of ${plural(r.samples, "answer")}`}
                  </span>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </ScrollX>
  );
}
