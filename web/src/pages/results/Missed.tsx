import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type Gap, type GapPair, type PageCheck, type ResultsFilter, type SourcePage } from "../../api";
import { andList, shortUrl, useLabels } from "../../lines";
import { keys, useAnsweredRuns, useSettings, useSurfaces } from "../../queries";
import { Badge, Button, Card, CardHeader, cx, EmptyState, ErrorNote, Icon, Pending, scrollArea, ScrollX, useLoading } from "../../ui";
import { useFilter, useView } from "../../view";
import { AgentPromptButton } from "./AgentPrompt";
import { HandledList, MarkedPages, MarkHandled } from "./Handled";
import { ViewHeader } from "./Header";
import { Instead } from "./Questions";
import { LeftOutBadge } from "./Scoring";
import { Tabs, useTab } from "./Tabs";

const tabs = [
  { id: "questions", label: "Missed questions" },
  { id: "handled", label: "Handled" },
  { id: "sources", label: "Pages AI links to" },
] as const;

/** AI answers > Missed questions: where an AI app left you out, what to do about it, and the pages AI reads. */
export function Missed() {
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
        Once a check has run, this lists the questions where an AI app did not name {brand}, and the pages AI links to.
      </EmptyState>
    );
  else if (tab === "questions") body = <MissedQuestions filter={filter} />;
  else if (tab === "handled") body = <HandledList />;
  else body = <LinkedPages filter={filter} />;

  return (
    <>
      <ViewHeader
        title="Missed questions"
        range={tab === "sources"}
      />
      <div className={cx("relative min-h-0 flex-1 px-3 py-4 md:px-5 md:py-5", scrollArea)}>
        <div className="mx-auto max-w-6xl space-y-4">
          <Tabs label="Missed questions views" tabs={tabs} current={tab} />
          {body}
        </div>
      </div>
    </>
  );
}

/** How many AI apps left you out. */
function MissedIn({ gap }: { gap: Gap }) {
  return (
    <span className="w-24 shrink-0 text-xs leading-tight font-medium">
      Missed in <span className="tabular-nums">{gap.engines - gap.named}</span> of <span className="tabular-nums">{gap.engines}</span>
    </span>
  );
}

/**
 * Which AI apps named you, by name: the shorter of the two lists, so a
 * question most apps missed lists the few that named you, and one most
 * named lists the few that missed you.
 */
function WhoNamedYou({ gap }: { gap: Gap }) {
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const named = gap.pairs.filter((p) => p.named);
  const missed = gap.pairs.filter((p) => !p.named);
  const names = (ps: GapPair[]) => andList(ps.map((p) => labels.line(p.pair)));
  if (named.length === 0) return <p className="text-xs text-danger">No AI app named you.</p>;
  if (named.length <= missed.length)
    return (
      <p className="text-xs text-muted">
        <span className="font-medium text-success">Named you:</span> {names(named)}
      </p>
    );
  return (
    <p className="text-xs text-muted">
      <span className="font-medium text-danger">Missed you:</span> {names(missed)}
    </p>
  );
}

/**
 * Pages to pitch that name each competitor, most linked first: the pages AI
 * reads where a competitor is and you are not.
 */
function pitchByCompetitor(pages: SourcePage[]): Map<string, SourcePage[]> {
  const by = new Map<string, SourcePage[]>();
  for (const p of pages) {
    if (!p.outreach || !p.check) continue;
    for (const c of p.check.competitors) by.set(c, [...(by.get(c) ?? []), p]);
  }
  for (const list of by.values()) list.sort((a, b) => b.citations - a.citations);
  return by;
}

/**
 * What to do about one missed question, built from who AI named and the
 * pages this question's missed answers linked to that name them and not you.
 */
function WhatToDo({ gap, pitch, run, filter }: { gap: Gap; pitch: Map<string, SourcePage[]>; run: number; filter: ResultsFilter }) {
  const names = gap.named_on_miss;
  const linked = new Set(gap.pairs.filter((p) => !p.named).flatMap((p) => p.links));
  const pages = [...new Map(names.flatMap((n) => pitch.get(n) ?? []).filter((p) => linked.has(p.url)).map((p) => [p.url, p])).values()].slice(0, 2);
  return (
    <div className="rounded-control bg-subtle px-2.5 py-2 text-xs">
      <span className="font-medium">What to do: </span>
      {names.length > 0 ? (
        <>
          AI named {andList(names.slice(0, 3))} instead.{" "}
          {pages.length > 0 ? (
            <>
              Its answers link to{" "}
              {pages.map((p, i) => (
                <span key={p.url}>
                  {i > 0 && " and "}
                  <a href={p.url} target="_blank" rel="noreferrer noopener" className="font-medium text-fg underline-offset-2 hover:underline [overflow-wrap:anywhere]">
                    {shortUrl(p.url)}
                  </a>
                </span>
              ))}
              , which {pages.length === 1 ? "names" : "name"} them and not you. Ask {pages.length === 1 ? "that site" : "those sites"} to include you, and write a page on your site that answers this question.
            </>
          ) : (
            "Write a page on your site that answers this question and says how you compare, so AI has something of yours to read and link to."
          )}
        </>
      ) : (
        "Write a page on your site that answers this question directly, so AI has something of yours to read and link to."
      )}
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <AgentPromptButton question={gap.question.id} run={run} filter={filter} />
        <MarkHandled question={gap.question.id} marked={gap.pages.length > 0} />
      </div>
    </div>
  );
}

function MissedQuestions({ filter }: { filter: ResultsFilter }) {
  const view = useView();
  const gaps = useQuery({ queryKey: keys.gaps(0, filter), queryFn: () => api.gaps(undefined, filter) });
  const sources = useQuery({ queryKey: keys.sources(filter), queryFn: () => api.sources(filter) });
  if (gaps.isPending || sources.isPending) return <Pending />;
  if (gaps.isError) return <ErrorNote error={gaps.error} />;
  const all = gaps.data.gaps;
  const pitch = pitchByCompetitor(sources.data?.pages ?? []);
  const hasDemand = all.some((g) => g.demand != null);
  const open = (g: Gap) => view.set({ q: g.question.id, qa: null, qv: null }, { push: true });

  return (
    <div className={cx("space-y-3", gaps.isPlaceholderData && "opacity-60")}>
      <p className="text-xs text-muted">
        Sorted by how many AI apps left you out{hasDemand ? ", then by how often people search for it on Google" : ""}. When two tie, the one whose answers named more competitors comes first.
      </p>
      {all.length === 0 ? (
        <Card className="px-4 py-10 text-center text-sm text-muted">
          {gaps.data.run_id ? `Every AI app in view named you in every question in check #${gaps.data.run_id}.` : "No answers from the AI apps in view yet."}
        </Card>
      ) : (
        <Card>
          <CardHeader title={`${all.length} missed ${all.length === 1 ? "question" : "questions"}`} description={`Check #${gaps.data.run_id}. Open a question to read each answer and the exact pages it linked to.`} />
          <ul className="divide-y divide-line border-t border-line">
            {all.map((g) => (
              <li key={g.question.id} className="group relative">
                <div className="flex gap-3 px-4 py-3 group-hover:bg-subtle sm:gap-4">
                  <MissedIn gap={g} />
                  <div className="min-w-0 flex-1 space-y-2">
                    <div className="min-w-0">
                      <button
                        type="button"
                        onClick={(e) => {
                          e.currentTarget.focus();
                          open(g);
                        }}
                        className="-my-3 py-3 text-left font-medium group-hover:underline after:absolute md:my-0 md:py-0 after:inset-0 focus-visible:outline-none focus-visible:after:outline-2 focus-visible:after:-outline-offset-2 focus-visible:after:outline-ring"
                      >
                        {g.question.text}
                      </button>
                      <div className="flex flex-wrap items-center gap-x-2 text-xs text-faint">
                        {g.question.group}
                        <LeftOutBadge question={g.question} />
                      </div>
                    </div>
                    <WhoNamedYou gap={g} />
                    <MarkedPages pages={g.pages} />
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-muted tabular-nums">
                      {hasDemand && (
                        <span>
                          Google searches a month <span className="font-medium text-fg">{g.demand == null ? "–" : g.demand.toLocaleString()}</span>
                        </span>
                      )}
                      {g.named_on_miss.length > 0 && (
                        <span className="flex flex-wrap items-center gap-1.5">
                          <span className="whitespace-nowrap">Named instead</span> <Instead names={g.named_on_miss} max={4} />
                        </span>
                      )}
                    </div>
                    <div className="relative z-[1]">
                      <WhatToDo gap={g} pitch={pitch} run={gaps.data.run_id} filter={filter} />
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

/** What each AI app searched Google for, for the apps that show it. */
function Searches({ pairs }: { pairs: GapPair[] }) {
  const surfaces = useSurfaces();
  const labels = useLabels(surfaces.data);
  const searched = pairs.filter((p) => p.fan_out && p.fan_out.length > 0);
  if (searched.length === 0) return null;
  const apps = [...new Set(searched.map((p) => labels.surfaceTitle(p.pair.surface)))];
  return (
    <div className="px-0.5 text-xs">
      <div className="mb-1 flex flex-wrap items-center gap-x-1.5 text-muted">
        <Icon name="search" className="size-3.5" />
        What the AI searched Google for
        <span className="text-faint">(only {andList(apps)} {apps.length === 1 ? "shows" : "show"} this)</span>
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

/** What a linked page says about you, in words, never colour alone. */
export function PageMark({ check }: { check: PageCheck | null }) {
  if (!check) return <span className="text-xs text-faint">Not read yet</span>;
  if (check.status !== "ok") {
    const why = { failed: "Couldn't open this page", disallowed: "The site doesn't allow reading", refused: "Not read" }[check.status];
    return <span className="text-xs text-faint">{why}</span>;
  }
  return check.self_present ? <Badge tone="success">Names you</Badge> : <span className="text-xs text-muted">Does not name you</span>;
}

/** Pages listed before Show all. */
const firstPages = 25;

function LinkedPages({ filter }: { filter: ResultsFilter }) {
  const view = useView();
  const [all, setAll] = useState(false);
  const sources = useQuery({ queryKey: keys.sources(filter), queryFn: () => api.sources(filter) });
  if (sources.isPending) return <Pending />;
  if (sources.isError) return <ErrorNote error={sources.error} />;
  const s = sources.data;
  if (s.citations === 0) return <Card className="px-4 py-10 text-center text-sm text-muted">No answer in this range linked to a page.</Card>;
  const pitchOnly = view.get("only") === "outreach";
  const pitchCount = s.pages.filter((p) => p.outreach).length;
  const pages = pitchOnly ? s.pages.filter((p) => p.outreach) : s.pages;

  return (
    <div className={cx("space-y-4", sources.isPlaceholderData && "opacity-60")}>
      <p className="text-xs text-muted">
        {s.citations} links in {s.citing} of {s.answered} answers, to {s.domain_count} {s.domain_count === 1 ? "site" : "sites"} and {s.page_count} pages. Byname reads each page after a check to see who it names. A page that
        builds itself in the browser can read as not naming you.
      </p>
      <Card>
        <CardHeader title="Sites AI links to most" description={s.domain_count > s.domains.length ? `The ${s.domains.length} most linked of ${s.domain_count}.` : undefined} />
        <ScrollX label="linked sites">
          <table className="w-full min-w-max text-sm">
            <thead className="border-y border-line text-xs text-muted">
              <tr>
                <th scope="col" className="px-4 py-2 text-left font-medium">Site</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Links</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Answers</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Pages</th>
                <th scope="col" className="px-4 py-2 text-right font-medium">Pages to pitch</th>
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
        <CardHeader title="Pages AI links to most" description="A page to pitch names a competitor and never you, on a site that is neither yours nor a competitor's. Ask the site to include you.">
          <label className="inline-flex h-11 items-center gap-2 text-xs text-muted md:h-auto">
            <input type="checkbox" checked={pitchOnly} onChange={(e) => view.set({ only: e.target.checked ? "outreach" : null })} className="size-3.5 accent-[var(--fg)]" />
            Pages to pitch only ({pitchCount})
          </label>
        </CardHeader>
        {pages.length === 0 ? (
          <p className="border-t border-line px-4 py-8 text-center text-sm text-muted">No linked page names a competitor without naming you.</p>
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
        <a href={p.url} target="_blank" rel="noreferrer noopener" className={cx("block truncate py-3 text-sm hover:underline md:py-0", p.is_self && "font-semibold")}>
          {shortUrl(p.url)}
        </a>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
          <span className="tabular-nums">
            Linked {p.citations} {p.citations === 1 ? "time" : "times"} in {p.answers} {p.answers === 1 ? "answer" : "answers"}
          </span>
          {p.is_self && <Badge>Your site</Badge>}
          {p.outreach && (
            <Badge tone="warn">
              <Icon name="mail" className="size-3" />
              Page to pitch
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
