import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useIsMutating, useMutation, useMutationState, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, type McpCategory, type McpGrade, type McpReview, type McpServer as Server, type McpTool } from "../../api";
import { Donut, Sparkline } from "../../charts";
import { useToast } from "../../components/Toaster";
import { ReadOnlyNote } from "../../config/forms";
import { money } from "../../money";
import { keys, useCan, useIsAdmin, useMcpServer } from "../../queries";
import { Badge, Button, Card, CardHeader, ConfirmButton, cx, Dialog, DialogBody, EmptyState, ErrorNote, Field, Icon, inputClass, Pending, RelativeTime, ScrollX, Spinner } from "../../ui";
import {
  docScore,
  duration,
  hintLabel,
  letterColor,
  letterTone,
  partialNote,
  reviewDone,
  reviewOpen,
  reviewStopped,
  scoresOverTime,
  shortfalls,
  statusLabel,
  statusTone,
  toolCount,
  transportLabel,
} from "./mcpserver/model";
import { useReauthMutation } from "./Reauth";
import { SettingsHeader, SettingsSubhead } from "./Settings";

const gradingHint = "Grading reads the server's metadata and tool list, never calls a tool, and takes up to 30 seconds.";

export function McpServer() {
  const server = useMcpServer();
  const canEdit = useCan("config:write");
  return (
    <>
      <SettingsHeader title="MCP server" />
      {!canEdit && <ReadOnlyNote>You can see the grade. An admin or editor can change the server and grade it again.</ReadOnlyNote>}
      {server.isPending ? <Pending /> : server.isError ? <ErrorNote error={server.error} /> : <Page s={server.data} canEdit={canEdit} />}
    </>
  );
}

// Every caller shares the in-flight state and newest error through the
// mutation key, so a grade started by the URL form shows on the button.
function useGrade() {
  const qc = useQueryClient();
  const { mutate } = useMutation({ mutationKey: keys.mcpServerGrade, mutationFn: api.mcpServer.grade, onSuccess: (s) => qc.setQueryData(keys.mcpServer, s) });
  const pending = useIsMutating({ mutationKey: keys.mcpServerGrade }) > 0;
  const error = useMutationState({ filters: { mutationKey: keys.mcpServerGrade }, select: (m) => m.state.error }).at(-1);
  return { run: () => mutate(), pending, error };
}

function Page({ s, canEdit }: { s: Server; canEdit: boolean }) {
  if (!s.url) {
    return (
      <div className="space-y-6">
        <EmptyState icon="plug" title="Grade your MCP server">
          Add the URL of your MCP server and Byname scores it from 0 to 100 against the MCP spec essentials: connection, sign-in, server metadata, tool definitions, tool documentation and how agents find it. Every check that falls short says how to fix it.
        </EmptyState>
        {canEdit && <UrlForm url="" />}
      </div>
    );
  }
  const g = s.latest;
  return (
    <div className="space-y-10">
      <section className="space-y-6">
        {canEdit ? (
          <UrlForm key={s.url} url={s.url} />
        ) : (
          <p className="text-sm">
            <span className="text-muted">Server </span>
            <span className="font-mono text-xs [overflow-wrap:anywhere]">{s.url}</span>
          </p>
        )}
        <TokenRow s={s} />
      </section>
      {g ? (
        <>
          <ScoreCard g={g} current={s.url} />
          <CategoryTiles categories={g.categories} review={s.review} />
          {g.categories.map((c) => (
            <CategoryCard key={c.id} c={c} />
          ))}
          {g.tools.length > 0 && (
            <>
              <Tools tools={g.tools} />
              <ReviewCard r={s.review} />
            </>
          )}
          <History s={s} />
        </>
      ) : (
        <NotGraded />
      )}
    </div>
  );
}

function GradeButton({ label = "Grade again" }: { label?: string }) {
  const grade = useGrade();
  if (!useCan("runs:write")) return null;
  return (
    <Button onClick={grade.run} disabled={grade.pending}>
      <Icon name="refresh" className={cx("size-3.5", grade.pending && "animate-spin")} />
      {grade.pending ? "Grading…" : label}
    </Button>
  );
}

function NotGraded() {
  return (
    <EmptyState icon="chart" title="Not graded yet" action={<GradeButton label="Grade now" />}>
      {gradingHint}
    </EmptyState>
  );
}

function UrlForm({ url }: { url: string }) {
  const qc = useQueryClient();
  const grade = useGrade();
  const canRun = useCan("runs:write");
  const [value, setValue] = useState(url);
  const save = useMutation({
    mutationFn: (v: string) => api.mcpServer.set(v),
    onSuccess: (s) => {
      qc.setQueryData(keys.mcpServer, s);
      if (canRun && s.url) grade.run();
    },
  });
  const trimmed = value.trim();
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (trimmed) save.mutate(trimmed);
  };
  const busy = save.isPending || grade.pending;
  return (
    <form onSubmit={submit} className="space-y-3">
      <Field label="Server URL" hint="The Streamable HTTP endpoint agents connect to, such as https://mcp.example.com/mcp. A URL on another host drops the stored token.">
        <input className={cx(inputClass, "font-mono text-[16px] md:text-xs")} value={value} onChange={(e) => setValue(e.target.value)} placeholder="https://mcp.example.com/mcp" type="url" autoCapitalize="none" spellCheck={false} />
      </Field>
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" variant="primary" disabled={!trimmed || busy}>
          {save.isPending ? "Saving…" : grade.pending ? "Grading…" : canRun ? "Save and grade" : "Save"}
        </Button>
        {url && (
          <ConfirmButton label="Remove server" confirmLabel="Remove the server and its token?" onConfirm={() => save.mutate("")} disabled={busy} className="ml-auto">
            <Icon name="trash" className="size-3.5" />
            Remove
          </ConfirmButton>
        )}
      </div>
      <ErrorNote error={save.error} />
    </form>
  );
}

function TokenRow({ s }: { s: Server }) {
  const qc = useQueryClient();
  const grade = useGrade();
  const admin = useIsAdmin();
  const [token, setToken] = useState("");
  const set = useReauthMutation({
    mutationFn: (v: string) => api.mcpServer.setToken(v),
    onSuccess: (st, v) => {
      setToken("");
      qc.setQueryData(keys.mcpServer, st);
      if (v) grade.run();
    },
  });
  const status = s.token_set ? <Badge tone="success">Token set</Badge> : <Badge tone="outline">No token</Badge>;
  if (!admin) {
    return (
      <p className="flex flex-wrap items-center gap-2 text-sm text-muted">
        {status}
        <span>Only an admin can set the token for a server behind sign-in.</span>
      </p>
    );
  }
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (token.trim()) set.mutate(token.trim());
  };
  return (
    <div className="rounded-surface border border-line p-3">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold">Token</h3>
        {status}
      </div>
      <p className="mb-3 text-xs text-muted">For a server behind sign-in. Byname sends it as a bearer token to the server URL and nowhere else, stores it encrypted and never shows it again.</p>
      <form onSubmit={submit} className="flex flex-col gap-2 sm:flex-row">
        <input
          aria-label="Bearer token"
          className={inputClass}
          type="password"
          autoComplete="off"
          spellCheck={false}
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder={s.token_set ? "Paste a new token to replace it" : "Paste a token"}
        />
        <div className="flex shrink-0 gap-2">
          <Button type="submit" disabled={!token.trim() || set.isPending}>
            {set.isPending ? "Saving…" : s.token_set ? "Replace" : "Save token"}
          </Button>
          {s.token_set && (
            <ConfirmButton label="Delete token" confirmLabel="Delete?" onConfirm={() => set.mutate("")} disabled={set.isPending}>
              <Icon name="x" className="size-3.5" />
              Delete
            </ConfirmButton>
          )}
        </div>
      </form>
      <div className="mt-2 empty:hidden">
        <ErrorNote error={set.error} />
      </div>
    </div>
  );
}

function ScoreCard({ g, current }: { g: McpGrade; current: string }) {
  const grade = useGrade();
  const note = partialNote(g.partial);
  const rows: [string, ReactNode][] = [
    ["Server", <span className="font-mono text-xs [overflow-wrap:anywhere]">{g.url}</span>],
    ["Graded", <RelativeTime iso={g.graded_at} />],
    ["Name", g.server.title || g.server.name || <span className="text-muted">Not given</span>],
    ["Protocol", g.server.protocol_version || <span className="text-muted">No session</span>],
    ["Transport", transportLabel[g.server.transport]],
    ["Session start", duration(g.server.negotiate_ms)],
  ];
  return (
    <Card>
      <div className="flex flex-col items-center gap-6 p-4 sm:flex-row sm:items-start">
        <Donut
          size={144}
          label={`Score ${g.score} of 100, grade ${g.grade}`}
          segments={[
            { key: "score", label: `Score ${g.score}`, value: g.score, color: letterColor(g.grade) },
            { key: "rest", label: `Missing ${100 - g.score}`, value: 100 - g.score, color: "var(--subtle-strong)" },
          ]}
        >
          <div>
            <div className="text-2xl font-semibold">{g.grade}</div>
            <div className="text-xs text-muted tabular-nums">{g.score} / 100</div>
          </div>
        </Donut>
        <div className="w-full min-w-0 flex-1">
          <dl className="divide-y divide-line text-sm">
            {rows.map(([k, v]) => (
              <div key={k} className="grid grid-cols-[7rem_minmax(0,1fr)] gap-3 py-1.5">
                <dt className="text-muted">{k}</dt>
                <dd className="min-w-0">{v}</dd>
              </div>
            ))}
          </dl>
        </div>
      </div>
      <div className="space-y-3 border-t border-line px-4 py-3">
        {note && (
          <p role="note" className="flex items-start gap-2 rounded-control bg-warn-soft px-3 py-2 text-sm text-warn">
            <Icon name="alert" className="mt-0.5 size-4 shrink-0" />
            <span>{note}</span>
          </p>
        )}
        {g.url !== current && <p className="text-sm text-muted">This grade is for the URL above. Grade again to grade the one you saved.</p>}
        <div className="flex flex-wrap items-center gap-3">
          <GradeButton />
          <span className="text-xs text-muted">{grade.pending ? "Grading. This takes up to 30 seconds." : gradingHint}</span>
        </div>
        <ErrorNote error={grade.error} />
      </div>
    </Card>
  );
}

function CategoryTiles({ categories, review }: { categories: McpCategory[]; review: McpReview }) {
  return (
    <section>
      <SettingsSubhead>Categories</SettingsSubhead>
      <p className="mb-3 text-sm text-muted">The score is the mean of the categories that count. Sign-in and discoverability are left out when there is nothing to assess.</p>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
        {categories.map((c) => (
          <a key={c.id} href={`#${c.id}`} className="flex flex-col gap-1 rounded-surface border border-line px-3 py-3 transition-colors hover:border-line-hover">
            <span className="text-xs font-medium text-muted">{c.title}</span>
            {c.score == null ? (
              <span className="text-lg font-semibold text-faint">Left out</span>
            ) : c.id === "documentation" && review.score != null ? (
              <span className="text-lg font-semibold tabular-nums">{docScore(c.score, review)}</span>
            ) : (
              <span className="text-2xl font-semibold tabular-nums">{c.score}</span>
            )}
            {c.note && <span className="text-xs text-muted">{c.note}</span>}
          </a>
        ))}
      </div>
    </section>
  );
}

function CategoryCard({ c }: { c: McpCategory }) {
  const counted = c.checks.filter((ch) => ch.status !== "skipped");
  const passed = counted.filter((ch) => ch.status === "pass").length;
  return (
    <section id={c.id} className="scroll-mt-4">
      <Card>
        <CardHeader title={c.title} description={c.score == null ? `Left out. ${c.note}.` : `${passed} of ${counted.length} checks pass${c.note ? `. ${c.note}` : ""}.`}>
          {c.score != null && <span className="text-lg font-semibold tabular-nums">{c.score}</span>}
        </CardHeader>
        <ul className="divide-y divide-line border-t border-line">
          {c.checks.map((ch) => (
            <li key={ch.id} className="flex items-start gap-3 px-4 py-2.5 text-sm">
              <Badge tone={statusTone[ch.status]} className="mt-0.5 w-14 justify-center">
                {statusLabel[ch.status]}
              </Badge>
              <div className="min-w-0 flex-1">
                <p className="font-medium">{ch.title}</p>
                <p className="text-xs text-muted [overflow-wrap:anywhere]">{ch.finding}</p>
                {ch.fix && (
                  <p className="mt-1 text-xs [overflow-wrap:anywhere]">
                    <span className="font-medium">Fix: </span>
                    {ch.fix}
                  </p>
                )}
              </div>
            </li>
          ))}
        </ul>
      </Card>
    </section>
  );
}

function Tools({ tools }: { tools: McpTool[] }) {
  return (
    <section>
      <SettingsSubhead>Tools</SettingsSubhead>
      <p className="mb-3 text-sm text-muted">
        Each tool's documentation score: its title, description, inputs, hints and output schema, as the rules read them. AI is a model's score for the title, description and inputs, once
        someone reviews them below.
      </p>
      <Card>
        <ScrollX label="tools">
          <table className="w-full min-w-[46rem] text-left text-sm">
            <thead className="text-xs text-muted">
              <tr className="border-b border-line">
                <th className="px-4 py-2 font-medium">Tool</th>
                <th className="py-2 pr-4 text-right font-medium">Docs</th>
                <th className="py-2 pr-4 text-right font-medium">
                  AI <span className="font-normal text-faint">advisory</span>
                </th>
                <th className="py-2 pr-4 font-medium">Hints</th>
                <th className="py-2 pr-4 font-medium">Falls short on</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {tools.map((t) => {
                const short = shortfalls(t);
                const r = t.review;
                const summary = [short.length > 0 && `${short.length} ${short.length === 1 ? "check" : "checks"}`, r && "AI notes"].filter(Boolean).join(" · ");
                return (
                  <tr key={t.key || t.name} className="align-top">
                    <td className="min-w-36 px-4 py-2">
                      <div className="font-mono text-xs [overflow-wrap:anywhere]">{t.name}</div>
                      {t.title && <div className="text-xs text-muted">{t.title}</div>}
                    </td>
                    <td className="py-2 pr-4 text-right font-medium tabular-nums">{t.score}</td>
                    <td className="py-2 pr-4 text-right tabular-nums">{r ? r.score : <span className="text-faint">–</span>}</td>
                    <td className="py-2 pr-4">
                      <div className="flex flex-wrap gap-1">
                        {t.hints.length ? t.hints.map((h) => <Badge key={h}>{hintLabel(h)}</Badge>) : <span className="text-xs text-faint">None</span>}
                      </div>
                    </td>
                    <td className="py-2 pr-4 text-xs">
                      {!summary ? (
                        <span className="text-faint">Nothing</span>
                      ) : (
                        <details>
                          <summary className="cursor-pointer text-muted hover:text-fg">{summary}</summary>
                          <ul className="mt-1 space-y-1">
                            {short.map((m) => (
                              <li key={m.check}>
                                <Badge tone={statusTone[m.status]}>{statusLabel[m.status]}</Badge> <span className="font-mono">{m.check}</span>
                                <span className="text-muted">: {m.finding}</span>
                              </li>
                            ))}
                            {r &&
                              (
                                [
                                  ["Title", r.title],
                                  ["Description", r.description],
                                  ["Inputs", r.inputs],
                                ] as const
                              ).map(([label, p]) => (
                                <li key={label}>
                                  <Badge tone="info">AI {p.score}</Badge> <span className="font-medium">{label}</span>
                                  {p.reason && <span className="text-muted">: {p.reason}</span>}
                                </li>
                              ))}
                          </ul>
                        </details>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </ScrollX>
      </Card>
    </section>
  );
}

function ReviewCard({ r }: { r: McpReview }) {
  const qc = useQueryClient();
  const toast = useToast();
  const canRun = useCan("runs:write");
  const admin = useIsAdmin();
  const [asking, setAsking] = useState(false);
  const start = useMutation({
    mutationFn: api.mcpServer.review,
    onSuccess: (s) => {
      setAsking(false);
      qc.setQueryData(keys.mcpServer, s);
    },
  });
  const open = reviewOpen(r.status);
  // A toast only for a review this page watched end, not for one that
  // ended before the page opened.
  const watched = useRef(open);
  useEffect(() => {
    if (open) watched.current = true;
    else if (watched.current) {
      watched.current = false;
      toast(reviewDone(r), r.status === "completed" ? "done" : "error");
    }
  }, [open]);
  const stopped = reviewStopped(r);
  const cost = money(r.estimate_usd, "about");
  return (
    <>
      <Card>
        <CardHeader
          title="AI review of tool docs"
          description="A model reads each tool's title, description and inputs, scores each from 0 to 100 and says what to change. It is advisory: the grade's score never moves."
        >
          {r.score != null && <span className="text-lg font-semibold tabular-nums">AI {r.score}</span>}
        </CardHeader>
        <div className="space-y-3 border-t border-line px-4 py-3 text-sm">
          {open ? (
            <p className="flex items-center gap-2 text-muted" aria-live="polite">
              <Spinner className="size-3.5 border" />
              {r.status === "queued" ? "Queued. The review starts within a minute." : `Reviewing: ${r.reviewed} of ${toolCount(r.tools)} done.`}
            </p>
          ) : (
            <p className="text-muted">
              {r.reviewed > 0 ? `${r.reviewed} of ${toolCount(r.tools)} reviewed. ` : ""}
              {r.estimate_tools > 0
                ? `Reviewing ${r.estimate_tools === r.tools ? "" : "the other "}${toolCount(r.estimate_tools)} costs ${cost}, one Treg call a tool, counted against the monthly spend cap.`
                : "Every tool has a review of its current definition. A tool is reviewed again only when its definition changes."}
              {r.estimate_tools === 100 && r.tools > 100 && " A review reads at most 100 tools, worst rule score first."}
            </p>
          )}
          {stopped && !open && (
            <p role="note" className="flex items-start gap-2 rounded-control bg-warn-soft px-3 py-2 text-warn">
              <Icon name="alert" className="mt-0.5 size-4 shrink-0" />
              <span>
                {stopped} {r.error && <span className="[overflow-wrap:anywhere]">{r.error}</span>}
              </span>
            </p>
          )}
          {!r.has_token && (
            <p className="text-muted">
              Reviewing needs a Treg token, which bills each call.{" "}
              {admin ? (
                <Link to="/settings/treg" className="font-medium text-fg underline underline-offset-2">
                  Add it under Settings &gt; Treg
                </Link>
              ) : (
                "An admin adds it under Settings > Treg."
              )}
            </p>
          )}
          {canRun && !open && r.estimate_tools > 0 && (
            <Button onClick={() => setAsking(true)} disabled={!r.has_token}>
              <Icon name="sparkles" className="size-3.5" />
              Review tool docs
            </Button>
          )}
          <ErrorNote error={start.error} />
        </div>
      </Card>
      <Dialog open={asking} onClose={() => setAsking(false)} title="Review tool docs?">
        <DialogBody>
          <div className="space-y-3">
            <p className="text-sm">
              A model reviews {toolCount(r.estimate_tools)} for {cost}, charged through Treg and counted against your monthly spend cap. It stops if the month reaches the cap.
            </p>
            <p className="text-sm text-muted">It runs in the background within a minute and shows in Check history. The grade's score stays as it is.</p>
            <ErrorNote error={start.error} />
            <div className="flex justify-end gap-2 pt-1">
              <Button variant="ghost" onClick={() => setAsking(false)}>
                Cancel
              </Button>
              <Button variant="primary" onClick={() => start.mutate()} disabled={start.isPending} data-autofocus>
                {start.isPending && <Spinner className="size-3.5 border" />}
                Review {toolCount(r.estimate_tools)}
              </Button>
            </div>
          </div>
        </DialogBody>
      </Dialog>
    </>
  );
}

function History({ s }: { s: Server }) {
  const shown = s.history.slice(0, 10);
  return (
    <section>
      <div className="mb-3 flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold">History</h3>
        {s.history.length > 1 && <Sparkline values={scoresOverTime(s.history)} />}
      </div>
      <Card>
        <ul className="divide-y divide-line">
          {shown.map((h) => (
            <li key={h.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 text-sm">
              <Badge tone={letterTone(h.grade)} className="w-8 justify-center">
                {h.grade}
              </Badge>
              <span className="w-8 font-medium tabular-nums">{h.score}</span>
              <RelativeTime iso={h.graded_at} className="text-muted" />
              {h.url !== s.url && <span className="min-w-0 font-mono text-xs text-faint [overflow-wrap:anywhere]">{h.url}</span>}
            </li>
          ))}
        </ul>
      </Card>
      {s.history.length > shown.length && <p className="mt-2 text-xs text-muted">Byname keeps the newest {s.history.length} grades. The list shows the newest {shown.length}.</p>}
    </section>
  );
}
