import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type Gap, type GapPair, type PageCheck, type ResultsFilter, type SourcePage } from "../../api";
import { useLabels } from "../../lines";
import { keys, useAnsweredRuns, useMe, useSettings, useSurfaces } from "../../queries";
import { Badge, Button, Card, CardHeader, cx, EmptyState, ErrorNote, Icon, Pending, ScrollX, useLoading } from "../../ui";
import { useFilter, useView } from "../../view";
import { ViewHeader } from "./Header";
import { Instead } from "./Prompts";
import { Tabs, useTab } from "./Tabs";

const tabs = [
  { id: "prompts", label: "Prompt gaps" },
  { id: "sources", label: "Cited sources" },
] as const;

export function Gaps() {
  const settings = useSettings();
  const runs = useAnsweredRuns();
  const { filter, ready } = useFilter();
  const tab = useTab(tabs);
  const brand = settings.data?.brand.name || "you";
  useLoading(settings.isPending);

  let body;
  if (runs.isPending || !ready) body = <Pending />;
  else if (runs.isError) body = <ErrorNote error={runs.error} />;
  else if (!runs.data?.length)
    body = (
      <EmptyState icon="alert" title="No checks yet">
        Once a check has run, this lists the questions where {brand} is not named and the pages the answers cite.
      </EmptyState>
    );
  else if (tab === "prompts") body = <PromptGaps filter={filter} />;
  else body = <CitedSources filter={filter} />;

  return (
    <>
      <ViewHeader
        title="Gaps"
        subtitle={tab === "prompts" ? `Questions where some surface or model did not name ${brand} in the latest check, biggest opportunity first.` : `The sites and pages AI answers cite, and whether each one names ${brand}.`}
        range={tab === "sources"}
      />
      <div className="relative min-h-0 flex-1 overflow-auto px-3 py-4 md:px-5 md:py-5">
        <div className="mx-auto max-w-6xl space-y-4">
          <Tabs label="Gaps views" tabs={tabs} current={tab} />
          {body}
        </div>
      </div>
    </>
  );
}

/** Brief generation is not built; the action shows where it will be, never as a working button. */
function CreateBrief() {
  const id = useId();
  return (
    <span tabIndex={0} aria-describedby={id} className="group relative inline-flex cursor-not-allowed rounded-control outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={(e) => e.stopPropagation()}>
      <Button size="sm" disabled aria-disabled="true" tabIndex={-1} className="opacity-60">
        <Icon name="pencil" className="size-3.5" />
        Create brief
      </Button>
      <span id={id} role="tooltip" className="pointer-events-none absolute right-0 bottom-full z-10 mb-1 hidden w-max rounded-surface bg-surface px-2 py-1 text-xs text-muted shadow-floating group-hover:block group-focus-visible:block">
        Coming later
      </span>
    </span>
  );
}

function Score({ value, max }: { value: number; max: number }) {
  return (
    <span className="flex shrink-0 flex-col items-start gap-1" title={`Opportunity ${value} of ${max}`}>
      <span className="text-lg leading-none font-semibold tabular-nums">
        {value}
        <span className="text-xs font-normal text-faint"> / {max}</span>
      </span>
      <span className="flex gap-0.5" aria-hidden="true">
        {Array.from({ length: max }, (_, i) => (
          <span key={i} className={cx("h-1 w-2.5 rounded-full", i < value ? "bg-fg" : "bg-subtle-strong")} />
        ))}
      </span>
    </span>
  );
}

function PromptGaps({ filter }: { filter: ResultsFilter }) {
  const view = useView();
  const me = useMe();
  const canWrite = me.data?.scopes.includes("config:write") ?? false;
  const gaps = useQuery({ queryKey: keys.gaps(0, filter), queryFn: () => api.gaps(undefined, filter), placeholderData: (p) => p });
  if (gaps.isPending) return <Pending />;
  if (gaps.isError) return <ErrorNote error={gaps.error} />;
  const list = gaps.data.gaps;
  const hasDemand = list.some((g) => g.demand != null);
  const max = hasDemand ? 5 : 3;
  const open = (g: Gap) => view.set({ q: g.question.id, qa: null, qv: null }, { push: true });

  return (
    <div className={cx("space-y-3", gaps.isPlaceholderData && "opacity-60")}>
      <p className="text-xs text-muted">
        {hasDemand
          ? "Opportunity runs from 0 to 5: the share of surfaces and models that did not name you, times 3, plus 2 for 1,000 or more monthly searches or 1 for 300 or more."
          : "Opportunity runs from 0 to 3: the share of surfaces and models that did not name you, times 3, rounded. No search demand source is set up, so the score uses this engine gap alone."}{" "}
        Ties go to the question whose answers named more competitors.
      </p>
      {list.length === 0 ? (
        <Card className="px-4 py-10 text-center text-sm text-muted">
          {gaps.data.run_id ? `Every surface and model in view named you on every question in check #${gaps.data.run_id}.` : "No answers on the surfaces and models in view yet."}
        </Card>
      ) : (
        <Card>
          <CardHeader title={`${list.length} prompt ${list.length === 1 ? "gap" : "gaps"}`} description={`Check #${gaps.data.run_id}. Open a question to read each answer.`} />
          <ul className="divide-y divide-line border-t border-line">
            {list.map((g) => (
              <li key={g.question.id}>
                <div role="button" tabIndex={0} onClick={() => open(g)} onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), open(g))} className="flex cursor-pointer gap-3 px-4 py-3 outline-none hover:bg-subtle focus-visible:bg-subtle sm:gap-4">
                  <Score value={g.opportunity} max={max} />
                  <div className="min-w-0 flex-1 space-y-2">
                    <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
                      <div className="min-w-0 flex-1">
                        <div className="font-medium">{g.question.text}</div>
                        <div className="text-xs text-faint">{g.question.group}</div>
                      </div>
                      {canWrite && (
                        <div className="shrink-0">
                          <CreateBrief />
                        </div>
                      )}
                    </div>
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-muted tabular-nums">
                      <span>
                        Visible on <span className="font-medium text-fg">{g.named}</span> of {g.engines}
                      </span>
                      {hasDemand && (
                        <span>
                          Monthly searches <span className="font-medium text-fg">{g.demand == null ? "–" : g.demand.toLocaleString()}</span>
                        </span>
                      )}
                      {g.named_on_miss.length > 0 && (
                        <span className="flex flex-wrap items-center gap-1.5">
                          <span className="whitespace-nowrap">Named instead</span> <Instead names={g.named_on_miss} max={4} />
                        </span>
                      )}
                    </div>
                    <Searches pairs={g.pairs} />
                  </div>
                </div>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </div>
  );
}

/** The web searches each engine ran, by pair; pairs whose endpoint keeps them hidden are counted, not listed. */
function Searches({ pairs }: { pairs: GapPair[] }) {
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const searched = pairs.filter((p) => p.fan_out && p.fan_out.length > 0);
  if (searched.length === 0) return <p className="text-xs text-faint">None of these surfaces and models expose the web searches they ran.</p>;
  return (
    <div className="rounded-control bg-subtle px-2.5 py-2 text-xs">
      <div className="mb-1 flex flex-wrap items-center gap-x-1.5 text-muted">
        <Icon name="search" className="size-3.5" />
        Web searches the engines ran
        <span className="text-faint">
          · {searched.length} of {pairs.length} expose them
        </span>
      </div>
      <ul className="space-y-1">
        {searched.map((p) => (
          <li key={p.answer_id} className="[overflow-wrap:anywhere]">
            <span className="font-medium">{labels.line(p.pair)}</span>
            {!p.named && <span className="text-faint"> (missed you)</span>}
            <span className="text-muted">: </span>
            {p.fan_out!.map((f, i) => (
              <span key={i}>
                {i > 0 && ", "}“{f}”
              </span>
            ))}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** What a cited page says about you, in words, never colour alone. */
export function PageMark({ check }: { check: PageCheck | null }) {
  if (!check) return <span className="text-xs text-faint" title="Byname fetches cited pages after each check">Not read yet</span>;
  if (check.status !== "ok") {
    const why = { failed: "Could not read", disallowed: "robots.txt says no", refused: "Not fetched" }[check.status];
    return (
      <span className="text-xs text-faint" title={check.error || why}>
        {why}
      </span>
    );
  }
  return check.self_present ? <Badge tone="success">Names you</Badge> : <span className="text-xs text-muted">Does not name you</span>;
}

/** Pages listed before Show all. */
const firstPages = 25;

function CitedSources({ filter }: { filter: ResultsFilter }) {
  const view = useView();
  const [all, setAll] = useState(false);
  const sources = useQuery({ queryKey: keys.sources(filter), queryFn: () => api.sources(filter), placeholderData: (p) => p });
  if (sources.isPending) return <Pending />;
  if (sources.isError) return <ErrorNote error={sources.error} />;
  const s = sources.data;
  if (s.citations === 0) return <Card className="px-4 py-10 text-center text-sm text-muted">No answer in this range cited a page.</Card>;
  const outreachOnly = view.get("only") === "outreach";
  const outreach = s.pages.filter((p) => p.outreach).length;
  const pages = outreachOnly ? s.pages.filter((p) => p.outreach) : s.pages;

  return (
    <div className={cx("space-y-4", sources.isPlaceholderData && "opacity-60")}>
      <p className="text-xs text-muted">
        {s.citations} citations in {s.citing} of {s.answered} answers, from {s.domain_count} {s.domain_count === 1 ? "site" : "sites"} and {s.page_count} pages. Whether a page names you is read from its HTML as served, so a page
        built in the browser can read as not naming you.
      </p>
      <Card>
        <CardHeader title="Most cited sites" description={s.domain_count > s.domains.length ? `The ${s.domains.length} most cited of ${s.domain_count}.` : undefined} />
        <ScrollX label="cited sites">
          <table className="w-full min-w-max text-sm">
            <thead className="border-y border-line text-xs text-muted">
              <tr>
                <th scope="col" className="px-4 py-2 text-left font-medium">Site</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Citations</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Answers</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Pages</th>
                <th scope="col" className="px-4 py-2 text-right font-medium">Outreach targets</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {s.domains.slice(0, 15).map((d) => (
                <tr key={d.domain}>
                  <td className="px-4 py-2">
                    <span className="flex items-center gap-2">
                      <span className={cx(d.is_self && "font-semibold")}>{d.domain || "unknown"}</span>
                      {d.is_self && <Badge>You</Badge>}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums">{d.citations}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{d.answers}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{d.pages}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{d.outreach || <span className="text-faint">–</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollX>
      </Card>
      <Card>
        <CardHeader title="Most cited pages" description="Pages that name a competitor and never you, on sites neither yours nor a competitor's, are outreach targets.">
          <label className="inline-flex items-center gap-2 text-xs text-muted">
            <input type="checkbox" checked={outreachOnly} onChange={(e) => view.set({ only: e.target.checked ? "outreach" : null })} className="size-3.5 accent-[var(--fg)]" />
            Outreach targets only ({outreach})
          </label>
        </CardHeader>
        {pages.length === 0 ? (
          <p className="border-t border-line px-4 py-8 text-center text-sm text-muted">No cited page names a competitor without naming you.</p>
        ) : (
          <ul className="divide-y divide-line border-t border-line">
            {(all ? pages : pages.slice(0, firstPages)).map((p) => (
              <PageRow key={p.url} page={p} />
            ))}
          </ul>
        )}
        {!all && pages.length > firstPages && (
          <div className="border-t border-line px-4 py-2.5">
            <Button size="sm" variant="ghost" onClick={() => setAll(true)}>
              Show all {pages.length} pages
            </Button>
          </div>
        )}
      </Card>
    </div>
  );
}

function PageRow({ page: p }: { page: SourcePage }) {
  const others = p.check?.brands.filter((b) => !p.check!.competitors.includes(b)) ?? [];
  return (
    <li className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-start sm:gap-4">
      <div className="min-w-0 flex-1">
        <a href={p.url} target="_blank" rel="noreferrer noopener" className={cx("block truncate text-sm hover:underline", p.is_self && "font-semibold")} title={p.url}>
          {p.url.replace(/^https?:\/\/(www\.)?/, "")}
        </a>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
          <span className="tabular-nums">
            Cited {p.citations} {p.citations === 1 ? "time" : "times"} in {p.answers} {p.answers === 1 ? "answer" : "answers"}
          </span>
          {p.is_self && <Badge>Your site</Badge>}
          {p.outreach && (
            <Badge tone="warn">
              <Icon name="mail" className="size-3" />
              Outreach target
            </Badge>
          )}
        </div>
      </div>
      <div className="flex shrink-0 flex-col gap-1 sm:items-end">
        <PageMark check={p.check} />
        {p.check && p.check.brands.length > 0 && (
          <span className="flex flex-wrap gap-1 sm:justify-end">
            {p.check.competitors.map((b) => (
              <Badge key={b} tone="outline">
                {b}
              </Badge>
            ))}
            {others.map((b) => (
              <Badge key={b}>{b}</Badge>
            ))}
          </span>
        )}
      </div>
    </li>
  );
}
