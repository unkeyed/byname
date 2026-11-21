import { useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Fidelity, type Pair, type PairTest, type Route, type RouteOption, type SearchControl, type Surface, type SurfaceGroup, type SurfaceInput, type SurfaceKey } from "../api";
import { fidelityInfo, usd } from "../lines";
import { keys, useCan, useIsAdmin, useSurfaces } from "../queries";
import { Badge, Button, Card, cx, ErrorNote, Icon, inputClass, labelClass, PageSpinner, RelativeTime, Spinner, type Tone } from "../ui";
import { CredentialForm, testTone, useCredentials } from "./Credentials";
import { Disclosure, FieldNote } from "./forms";

export const groupInfo: Record<SurfaceGroup, { title: string; detail: string }> = {
  apps: { title: "Consumer apps and search", detail: "The answer a person sees in the app or on the results page, read by a scraping provider." },
  developer_tools: { title: "Developer tools", detail: "Tools that run in a terminal, matched by the same model with the same web search." },
  approximations: { title: "Approximations", detail: "Apps with no faithful source yet. The closest API stands in, always labeled and charted apart." },
};

export const routeInfo: Record<Route, { title: string; detail: string }> = {
  treg: { title: "Treg", detail: "The default. One Treg token runs every surface." },
  scraper: { title: "Your own scraping key", detail: "The same scraping provider, called with your key. App surfaces stay App answers." },
  provider: { title: "Your own provider key", detail: "The model provider's API, called with your key. On an app surface this makes an Approximation." },
  gateway: { title: "OpenAI-compatible gateway", detail: "Such as OpenRouter or Vercel AI Gateway. On an app surface this makes an Approximation. Not yet a tested integration." },
};

export const searchInfo: Record<SearchControl, string> = {
  by_app: "The app decides whether to search.",
  forceable: "Web search is forced on every call.",
  enable_only: "Web search is turned on. This route cannot force it, so some answers may not search.",
  always: "Searches on every call.",
  by_model: "Web search is forced where the model allows it. A model that refuses it is an Approximation.",
};

export const fidelityTone = (f: Fidelity): Tone => (f === "approximation" ? "warn" : f === "app" ? "success" : "info");

export function FidelityBadge({ fidelity, expected }: { fidelity: Fidelity; expected?: boolean }) {
  return (
    <Badge tone={fidelityTone(fidelity)} title={fidelityInfo[fidelity].detail}>
      {fidelityInfo[fidelity].label}
      {expected && <span className="font-normal opacity-80">, expected</span>}
    </Badge>
  );
}

/** What the person picks for a surface before saving it. */
interface Draft {
  enabled: boolean;
  route: Route;
  base_url: string;
  /** force is whether web search is forced, where the route can force it. */
  models: { model: string; force: boolean }[];
}

const fromServer = (s: Surface): Draft => ({ enabled: s.enabled, route: s.route, base_url: s.base_url, models: s.pairs.filter((p) => p.model).map((p) => ({ model: p.model, force: p.force_web_search })) });
const modelsKey = (d: Draft) => d.models.map((m) => `${m.model}:${m.force}`).join("\n");
const same = (a: Draft, b: Draft) => a.enabled === b.enabled && a.route === b.route && a.base_url === b.base_url && modelsKey(a) === modelsKey(b);
const optionFor = (s: Surface, route: Route): RouteOption =>
  s.routes.find((r) => r.route === route) ?? { route: s.route, credential: s.credential, provider: "", fidelity: s.fidelity, takes_model: s.takes_model, search: s.search };

function draftProblem(s: Surface, d: Draft): string | null {
  if (!d.enabled) return null;
  if (d.route === "gateway" && !/^https?:\/\/\S+$/.test(d.base_url.trim())) return "Give the gateway's base URL, such as https://openrouter.ai/api/v1.";
  if (optionFor(s, d.route).takes_model && d.models.length === 0) return `Pick at least one model for ${s.title}.`;
  return null;
}

function toInput(s: Surface, d: Draft): SurfaceInput {
  const opt = optionFor(s, d.route);
  return {
    enabled: d.enabled,
    route: d.route !== s.route ? d.route : undefined,
    base_url: d.route === "gateway" ? d.base_url.trim() : undefined,
    // A new route that takes a model needs the models, even on a surface being turned off.
    models:
      opt.takes_model && (d.enabled || d.route !== s.route)
        ? d.models.map((m) => ({ model: m.model, force_web_search: opt.search === "forceable" ? m.force : undefined }))
        : undefined,
  };
}

const testable = (s: Surface) => (s.enabled ? s.pairs.filter((p) => p.enabled) : []);

/** Whether onboarding may move on: every enabled pair saved and its last test passed. */
export interface Readiness {
  ok: boolean;
  reason?: string;
}

/**
 * Where people ask: onboarding step 4 and Settings > Where people ask. Ticks,
 * models and routes are drafts until Save; tests run against what is saved.
 */
export function WherePeopleAsk({ prefillDefaults, footer }: { prefillDefaults?: boolean; footer?: (ready: Readiness) => ReactNode }) {
  const surfaces = useSurfaces();
  if (surfaces.isPending) return <PageSpinner />;
  if (surfaces.isError) return <ErrorNote error={surfaces.error} />;
  return <Body surfaces={surfaces.data ?? []} prefillDefaults={prefillDefaults} footer={footer} />;
}

function Body({ surfaces, prefillDefaults, footer }: { surfaces: Surface[]; prefillDefaults?: boolean; footer?: (ready: Readiness) => ReactNode }) {
  const qc = useQueryClient();
  const canEdit = useCan("config:write");
  const [drafts, setDrafts] = useState<Record<SurfaceKey, Draft>>(() =>
    prefillDefaults && !surfaces.some((s) => s.enabled) ? Object.fromEntries(surfaces.filter((s) => s.default_on).map((s) => [s.key, { ...fromServer(s), enabled: true }])) : {},
  );
  const [advanced, setAdvanced] = useState<Record<SurfaceKey, boolean>>({});
  const [saveErrors, setSaveErrors] = useState<Record<SurfaceKey, unknown>>({});
  const [saving, setSaving] = useState(false);
  const [tests, setTests] = useState<Record<number, PairTest>>({});
  const [testErrors, setTestErrors] = useState<Record<number, unknown>>({});
  const [testing, setTesting] = useState<Set<number>>(new Set());
  const [tried, setTried] = useState(false);

  const draftOf = (s: Surface) => drafts[s.key] ?? fromServer(s);
  const dirty = surfaces.filter((s) => drafts[s.key] && !same(drafts[s.key], fromServer(s)));
  const setDraft = (s: Surface, patch: Partial<Draft>) => {
    setDrafts((all) => ({ ...all, [s.key]: { ...(all[s.key] ?? fromServer(s)), ...patch } }));
    setSaveErrors(({ [s.key]: _, ...rest }) => rest);
  };
  const putSurface = (next: Surface) => qc.setQueryData<Surface[]>(keys.surfaces, (list) => list?.map((x) => (x.key === next.key ? next : x)));
  const putPair = (pair: Pair) =>
    qc.setQueryData<Surface[]>(keys.surfaces, (list) => list?.map((x) => (x.key === pair.surface ? { ...x, pairs: x.pairs.map((p) => (p.id === pair.id ? pair : p)) } : x)));

  const saveAll = async () => {
    setTried(true);
    if (dirty.some((s) => draftProblem(s, drafts[s.key]))) return;
    setSaving(true);
    const errors: Record<SurfaceKey, unknown> = {};
    for (const s of dirty) {
      try {
        putSurface(await api.surfaces.put(s.key, toInput(s, drafts[s.key])));
        setDrafts(({ [s.key]: _, ...rest }) => rest);
      } catch (err) {
        errors[s.key] = err;
      }
    }
    setSaveErrors(errors);
    setSaving(false);
    setTried(false);
    void qc.invalidateQueries({ queryKey: keys.surfaces });
    void qc.invalidateQueries({ queryKey: keys.estimate });
    void qc.invalidateQueries({ queryKey: keys.credentials });
  };

  const runTest = async (pair: Pair) => {
    setTesting((t) => new Set(t).add(pair.id));
    setTestErrors(({ [pair.id]: _, ...rest }) => rest);
    try {
      const result = await api.pairs.test(pair.id);
      setTests((t) => ({ ...t, [pair.id]: result }));
      putPair(result.pair);
    } catch (err) {
      setTestErrors((e) => ({ ...e, [pair.id]: err }));
    } finally {
      setTesting((t) => {
        const next = new Set(t);
        next.delete(pair.id);
        return next;
      });
    }
  };
  const allPairs = surfaces.flatMap(testable);
  const testAll = () => Promise.all(allPairs.filter((p) => !testing.has(p.id)).map(runTest)).then(() => qc.invalidateQueries({ queryKey: keys.estimate }));

  const enabledCount = surfaces.filter((s) => draftOf(s).enabled).length;
  const needsTreg = surfaces.some((s) => draftOf(s).enabled && draftOf(s).route === "treg");
  const failing = allPairs.filter((p) => p.test_status !== "ok");
  const ready: Readiness =
    dirty.length > 0
      ? { ok: false, reason: "Save your changes first." }
      : !surfaces.some((s) => s.enabled)
        ? { ok: false, reason: "Tick at least one surface and save." }
        : failing.length > 0
          ? { ok: false, reason: `Test every surface and model. ${failing.length} of ${allPairs.length} ${allPairs.length === 1 ? "has" : "have"} not passed yet.` }
          : { ok: true };

  const groups = (Object.keys(groupInfo) as SurfaceGroup[]).map((g) => ({ g, list: surfaces.filter((s) => s.group === g) })).filter((x) => x.list.length > 0);

  return (
    <div className="space-y-6">
      <TregToken needed={needsTreg} onNoTreg={() => setAdvanced(Object.fromEntries(surfaces.map((s) => [s.key, true])))} canEdit={canEdit} />

      {groups.map(({ g, list }) => (
        <section key={g} aria-labelledby={`group-${g}`}>
          <h3 id={`group-${g}`} className="text-sm font-semibold">
            {groupInfo[g].title}
          </h3>
          <p className="mb-2 text-xs text-muted">{groupInfo[g].detail}</p>
          <Card>
            <ul className="divide-y divide-line">
              {list.map((s) => (
                <SurfaceRow
                  key={s.key}
                  s={s}
                  draft={draftOf(s)}
                  dirty={dirty.includes(s)}
                  canEdit={canEdit}
                  onChange={(patch) => setDraft(s, patch)}
                  advancedOpen={advanced[s.key] ?? false}
                  onAdvanced={(open) => setAdvanced((a) => ({ ...a, [s.key]: open }))}
                  problem={tried ? draftProblem(s, draftOf(s)) : null}
                  saveError={saveErrors[s.key]}
                  tests={tests}
                  testErrors={testErrors}
                  testing={testing}
                  onTest={runTest}
                />
              ))}
            </ul>
          </Card>
        </section>
      ))}

      {canEdit && (
        <div className="sticky bottom-0 z-10 -mx-1 bg-bg/90 px-1 py-3 backdrop-blur">
          <div className="flex flex-wrap items-center gap-2">
            <p className="mr-auto text-xs text-muted" aria-live="polite">
              {dirty.length > 0
                ? `${dirty.length} unsaved ${dirty.length === 1 ? "change" : "changes"}. ${enabledCount} ${enabledCount === 1 ? "surface" : "surfaces"} ticked.`
                : allPairs.length > 0
                  ? `${allPairs.length - failing.length} of ${allPairs.length} surface and model ${allPairs.length === 1 ? "pair" : "pairs"} passed a test.`
                  : "Tick the surfaces your buyers use, then save."}
            </p>
            {dirty.length > 0 && (
              <Button variant="ghost" onClick={() => setDrafts({})} disabled={saving}>
                Discard
              </Button>
            )}
            {dirty.length > 0 ? (
              <Button variant="primary" onClick={() => void saveAll()} disabled={saving}>
                {saving ? "Saving…" : "Save changes"}
              </Button>
            ) : (
              allPairs.length > 0 && (
                <Button onClick={() => void testAll()} disabled={testing.size > 0}>
                  {testing.size > 0 ? <Spinner className="size-3 border" /> : <Icon name="check" className="size-3.5" />}
                  Test all
                </Button>
              )
            )}
          </div>
          {footer?.(ready)}
        </div>
      )}
    </div>
  );
}

function TregToken({ needed, onNoTreg, canEdit }: { needed: boolean; onNoTreg: () => void; canEdit: boolean }) {
  const admin = useIsAdmin();
  const creds = useCredentials();
  const [replacing, setReplacing] = useState(false);
  const treg = creds.data?.find((c) => c.kind === "treg");
  return (
    <Card className="p-4">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold">Treg token</h3>
        {treg && (treg.set ? <Badge tone="success">Set</Badge> : needed ? <Badge tone="warn">Required</Badge> : <Badge>Not needed</Badge>)}
        {treg?.last_test_status && (
          <Badge tone={testTone(treg.last_test_status)} title={treg.last_test_status}>
            {treg.last_test_status === "ok" ? "Test passed" : "Test failed"}
          </Badge>
        )}
        {canEdit && (
          <button type="button" onClick={onNoTreg} className="ml-auto text-xs text-muted underline underline-offset-2 hover:text-fg">
            Don&apos;t use Treg?
          </button>
        )}
      </div>
      <p className="mb-3 text-xs text-muted">
        One token runs every surface below. It is stored encrypted and only ever sent to Treg.{" "}
        {needed ? "Required while any ticked surface goes through Treg." : "Not needed: every ticked surface uses its own key or a gateway."}
      </p>
      {admin ? (
        creds.isPending ? (
          <PageSpinner />
        ) : creds.isError ? (
          <ErrorNote error={creds.error} />
        ) : treg?.set && !replacing ? (
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="text-muted">
              Saved {treg.updated_at && <RelativeTime iso={treg.updated_at} />}
              {treg.org && <> for org {treg.org}</>}. It is never shown again.
            </span>
            <Button size="sm" onClick={() => setReplacing(true)}>
              Replace
            </Button>
          </div>
        ) : (
          <CredentialForm kind="treg" title="Treg" set={!!treg?.set} org={treg?.org} compact hint={treg?.hint} onDone={() => setReplacing(false)} />
        )
      ) : (
        <p className="text-xs text-muted">An admin pastes the Treg token, here or under Settings &gt; Credentials.</p>
      )}
    </Card>
  );
}

function SurfaceRow({
  s,
  draft,
  dirty,
  canEdit,
  onChange,
  advancedOpen,
  onAdvanced,
  problem,
  saveError,
  tests,
  testErrors,
  testing,
  onTest,
}: {
  s: Surface;
  draft: Draft;
  dirty: boolean;
  canEdit: boolean;
  onChange: (patch: Partial<Draft>) => void;
  advancedOpen: boolean;
  onAdvanced: (open: boolean) => void;
  problem: string | null;
  saveError: unknown;
  tests: Record<number, PairTest>;
  testErrors: Record<number, unknown>;
  testing: Set<number>;
  onTest: (p: Pair) => void;
}) {
  const opt = optionFor(s, draft.route);
  const pairs = !dirty ? testable(s) : [];
  const id = `surface-${s.key}`;
  return (
    <li className="px-4 py-3">
      <div className="flex items-start gap-3">
        <input id={id} type="checkbox" className="mt-1" checked={draft.enabled} disabled={!canEdit} onChange={(e) => onChange({ enabled: e.target.checked })} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <label htmlFor={id} className="font-medium">
              {s.title}
            </label>
            <FidelityBadge fidelity={opt.fidelity} />
            {draft.route !== "treg" && <Badge tone="outline">{opt.provider || routeInfo[draft.route].title}</Badge>}
            {dirty && <Badge tone="info">Unsaved</Badge>}
          </div>
          <p className="mt-0.5 text-xs text-muted">{fidelityInfo[opt.fidelity].detail}</p>
        </div>
      </div>

      {draft.enabled && (
        <div className="mt-3 space-y-3 sm:pl-7">
          {opt.takes_model ? (
            <ModelChips s={s} draft={draft} opt={opt} canEdit={canEdit} onChange={(models) => onChange({ models })} />
          ) : (
            <p className="text-xs text-muted">The app picks the model. Byname stores the model name when the answer reports one.</p>
          )}
          <p className="text-xs text-muted">{searchInfo[opt.search]}</p>

          {pairs.length > 0 && (
            <ul className="divide-y divide-line rounded-surface border border-line">
              {pairs.map((p) => (
                <PairRow key={p.id} p={p} result={tests[p.id]} error={testErrors[p.id]} testing={testing.has(p.id)} canEdit={canEdit} onTest={() => onTest(p)} />
              ))}
            </ul>
          )}
          {dirty && s.enabled && <p className="text-xs text-muted">Save to test the new setup.</p>}

          <Disclosure summary="Advanced: own key or gateway" open={advancedOpen} onToggle={onAdvanced}>
            <RoutePicker s={s} draft={draft} canEdit={canEdit} onChange={onChange} />
          </Disclosure>
        </div>
      )}
      {problem && (
        <div className="sm:pl-7">
          <FieldNote tone="error">{problem}</FieldNote>
        </div>
      )}
      {saveError != null && (
        <div className="mt-2 sm:pl-7">
          <ErrorNote error={saveError} />
        </div>
      )}
    </li>
  );
}

function ModelChips({ s, draft, opt, canEdit, onChange }: { s: Surface; draft: Draft; opt: RouteOption; canEdit: boolean; onChange: (models: Draft["models"]) => void }) {
  const creds = useCredentials();
  const cred = creds.data?.find((c) => c.kind === opt.credential);
  // Admins know whether the route's key is set; asking for models without it is a 400.
  const keyMissing = cred ? !cred.set : false;
  const ready = draft.route !== "gateway" || /^https?:\/\/\S+$/.test(draft.base_url.trim());
  const models = useQuery({
    queryKey: keys.models(s.key, draft.route, draft.route === "gateway" ? draft.base_url.trim() : ""),
    queryFn: async () => (await api.surfaces.models(s.key, draft.route, draft.route === "gateway" ? draft.base_url.trim() : undefined)) ?? [],
    enabled: ready && !keyMissing,
    staleTime: 5 * 60_000,
    retry: false,
  });
  const picked = new Set(draft.models.map((m) => m.model));
  const toggle = (name: string) =>
    onChange(picked.has(name) ? draft.models.filter((m) => m.model !== name) : [...draft.models, { model: name, force: opt.search === "forceable" }]);
  const setForce = (name: string, force: boolean) => onChange(draft.models.map((m) => (m.model === name ? { ...m, force } : m)));
  const listed = new Set((models.data ?? []).map((m) => m.name));
  const offList = draft.models.map((m) => m.model).filter((m) => !listed.has(m));
  return (
    <fieldset>
      <legend className={labelClass}>
        Models <span className="font-normal text-faint">· pick one or more, each becomes its own series</span>
      </legend>
      {!ready ? (
        <p className="text-xs text-muted">Give the gateway&apos;s base URL under Advanced to list its models.</p>
      ) : keyMissing && cred ? (
        <p className="text-xs text-muted">
          Models load once the {cred.title} {cred.kind === "treg" ? "token" : "key"} is set{cred.kind === "treg" ? " above" : " under Advanced"}.
        </p>
      ) : models.isPending ? (
        <Spinner className="size-3.5" />
      ) : models.isError ? (
        <div className="space-y-1">
          <ErrorNote error={models.error} />
          {opt.route === "treg" && <p className="text-xs text-muted">Models load once the Treg token is set.</p>}
        </div>
      ) : (
        <div className="flex flex-wrap gap-1.5">
          {[...offList.map((name) => ({ name, web_search: true, off: true })), ...(models.data ?? []).map((m) => ({ ...m, off: false }))].map((m) => {
            const on = picked.has(m.name);
            return (
              <button
                key={m.name}
                type="button"
                aria-pressed={on}
                disabled={!canEdit}
                onClick={() => toggle(m.name)}
                title={m.off ? "Not on the route's current list" : m.web_search ? "Supports web search" : "No web search on this model"}
                className={cx(
                  "inline-flex h-7 items-center gap-1 rounded-full border px-2.5 font-mono text-xs transition-colors disabled:cursor-default",
                  on ? "border-fg bg-fg text-bg" : "border-line-strong text-muted hover:border-line-hover hover:text-fg",
                  m.off && "border-dashed",
                )}
              >
                {on && <Icon name="check" className="size-3" />}
                {m.name}
                {!m.web_search && <span className="font-sans opacity-70">· no search</span>}
              </button>
            );
          })}
          {models.data?.length === 0 && offList.length === 0 && <p className="text-xs text-muted">This route lists no models.</p>}
        </div>
      )}
      {opt.search === "forceable" && draft.models.length > 0 && (
        <ul className="mt-2 space-y-1" aria-label="Forced web search per model">
          {draft.models.map((m) => (
            <li key={m.model}>
              <label className="flex flex-wrap items-center gap-x-2 text-xs">
                <input type="checkbox" checked={m.force} disabled={!canEdit} onChange={(e) => setForce(m.model, e.target.checked)} />
                <span>
                  Force web search on <span className="font-mono">{m.model}</span>
                </span>
                {!m.force && <span className="text-warn">Without it the answer may not search, so this pair is an Approximation.</span>}
              </label>
            </li>
          ))}
        </ul>
      )}
    </fieldset>
  );
}

const statusBadge: Record<PairTest["status"], { label: string; tone: Tone }> = {
  ok: { label: "Passed", tone: "success" },
  no_answer: { label: "No answer", tone: "danger" },
  failed: { label: "Failed", tone: "danger" },
};

function PairRow({ p, result, error, testing, canEdit, onTest }: { p: Pair; result?: PairTest; error: unknown; testing: boolean; canEdit: boolean; onTest: () => void }) {
  const tested = p.test_status !== "";
  const failed = result ? result.status !== "ok" : tested && p.test_status !== "ok";
  return (
    <li className="space-y-1.5 px-3 py-2">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className={cx("min-w-0 text-sm [overflow-wrap:anywhere]", p.model ? "font-mono text-xs" : "text-muted")}>{p.model || "The app picks the model"}</span>
        <FidelityBadge fidelity={p.fidelity} expected={!tested} />
        {p.force_web_search && <Badge tone="outline">search forced</Badge>}
        {p.paused_reason && (
          <Badge tone="warn" title={p.paused_reason}>
            Paused
          </Badge>
        )}
        <span className="ml-auto flex items-center gap-2">
          {result ? (
            <Badge tone={statusBadge[result.status].tone}>{statusBadge[result.status].label}</Badge>
          ) : tested ? (
            <Badge tone={p.test_status === "ok" ? "success" : "danger"}>{p.test_status === "ok" ? "Passed" : "Failed"}</Badge>
          ) : (
            <Badge>Not tested</Badge>
          )}
          {canEdit && (
            <Button size="sm" variant="ghost" onClick={onTest} disabled={testing}>
              {testing ? <Spinner className="size-3 border" /> : null}
              {testing ? "Testing…" : tested ? "Test again" : "Test"}
            </Button>
          )}
        </span>
      </div>
      {result ? (
        <TestDetails r={result} />
      ) : (
        tested && (
          <p className="text-xs text-muted [overflow-wrap:anywhere]">
            {p.test_status === "ok" ? (
              <>
                {p.test_searched === true ? "Searched. " : p.test_searched === false ? "Did not search. " : ""}
                {fidelityInfo[p.fidelity].label} confirmed
              </>
            ) : (
              <span className="text-danger">{p.test_status}</span>
            )}
            {p.last_tested_at && (
              <>
                {" "}
                · tested <RelativeTime iso={p.last_tested_at} />
              </>
            )}
          </p>
        )
      )}
      {!tested && !result && <p className="text-xs text-faint">The label is what this route should give. A test confirms it, or lowers it to Approximation.</p>}
      {p.paused_reason && <p className="text-xs text-warn">{p.paused_reason}</p>}
      {failed && p.force_web_search && (
        <p className="text-xs text-muted">
          If the model refused forced web search, untick “Force web search” for it above, save, and test again. The pair then counts as an Approximation.
        </p>
      )}
      <ErrorNote error={error} />
    </li>
  );
}

function TestDetails({ r }: { r: PairTest }) {
  const facts = [
    r.cost_reported && r.cost_usd != null ? `Cost ${usd(r.cost_usd)}` : "Cost not reported (billed by tokens)",
    r.searched ? "searched" : "did not search",
    r.cited ? `cited ${r.citations} ${r.citations === 1 ? "link" : "links"}` : "cited nothing",
    r.reported_model ? `reported model ${r.reported_model}` : "no model reported",
  ];
  return (
    <div className="space-y-0.5 text-xs text-muted">
      {r.status !== "ok" && r.error && <p className="text-danger [overflow-wrap:anywhere]">{r.error}</p>}
      <p className="[overflow-wrap:anywhere]">{facts.join(" · ")}</p>
      {r.status === "ok" && (
        <p>
          Confirmed <span className="font-medium text-fg">{fidelityInfo[r.pair.fidelity].label}</span>.
        </p>
      )}
    </div>
  );
}

function RoutePicker({ s, draft, canEdit, onChange }: { s: Surface; draft: Draft; canEdit: boolean; onChange: (patch: Partial<Draft>) => void }) {
  const admin = useIsAdmin();
  const creds = useCredentials();
  const opt = optionFor(s, draft.route);
  const cred = creds.data?.find((c) => c.kind === opt.credential);
  const current = optionFor(s, s.route);
  const changed = draft.route !== s.route;
  return (
    <>
      <fieldset>
        <legend className={labelClass}>How {s.title} is asked</legend>
        <div className="space-y-1.5">
          {s.routes.map((r) => (
            <label key={r.route} className="flex cursor-pointer items-start gap-2 text-sm has-disabled:cursor-default">
              <input
                type="radio"
                className="mt-0.5"
                name={`route-${s.key}`}
                checked={draft.route === r.route}
                disabled={!canEdit}
                onChange={() => onChange({ route: r.route, models: r.takes_model ? draft.models.map((m) => ({ ...m, force: r.search === "forceable" })) : [] })}
              />
              <span className="min-w-0">
                <span className="font-medium">{routeInfo[r.route].title}</span>
                {r.route !== "treg" && r.provider && <span className="text-muted"> · {r.provider}</span>}
                <span className="ml-1.5 inline-block align-middle">
                  <FidelityBadge fidelity={r.fidelity} />
                </span>
                <span className="block text-xs text-muted">{routeInfo[r.route].detail}</span>
              </span>
            </label>
          ))}
        </div>
      </fieldset>

      {draft.route === "gateway" && (
        <label className="block">
          <span className={labelClass}>Gateway base URL</span>
          <input
            className={inputClass}
            value={draft.base_url}
            disabled={!canEdit}
            onChange={(e) => onChange({ base_url: e.target.value })}
            placeholder="https://openrouter.ai/api/v1"
            inputMode="url"
            autoCapitalize="none"
            spellCheck={false}
          />
        </label>
      )}

      <div className="rounded-control bg-subtle px-3 py-2 text-xs">
        {changed ? (
          <p className="flex flex-wrap items-center gap-1.5">
            Fidelity once saved: <FidelityBadge fidelity={current.fidelity} /> <Icon name="right" className="size-3" /> <FidelityBadge fidelity={opt.fidelity} />
            <span className="basis-full text-muted">Saving resets this surface&apos;s tests, and its answers start a new series.</span>
          </p>
        ) : (
          <p className="flex flex-wrap items-center gap-1.5">
            Fidelity on this route: <FidelityBadge fidelity={opt.fidelity} />
          </p>
        )}
      </div>

      {draft.route === "treg" ? (
        <p className="text-xs text-muted">Uses the Treg token above.</p>
      ) : admin ? (
        cred?.set ? (
          <p className="text-xs text-muted">
            The {cred.title} key is set. Replace or test it under Settings &gt; Credentials.
          </p>
        ) : (
          <CredentialForm kind={opt.credential} title={cred?.title ?? opt.provider} hint={cred?.hint} set={false} compact />
        )
      ) : (
        <p className="text-xs text-muted">This route calls with the {opt.provider || routeInfo[draft.route].title} key. An admin must set it under Settings &gt; Credentials.</p>
      )}
    </>
  );
}
