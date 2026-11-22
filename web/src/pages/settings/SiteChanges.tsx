import { useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, type ChangeSettings, type PathRule, type RepoStatus } from "../../api";
import { NumberField, SubmitButton } from "../../config/forms";
import { formatNumber, parseNumber } from "../../config/rules";
import { keys, useChangeSettings, useRepo, useSettings } from "../../queries";
import { Badge, Button, Card, ConfirmButton, cx, ErrorNote, Field, Icon, inputClass, Pending, RelativeTime } from "../../ui";
import { useZone } from "../changes/model";
import { useReauthMutation } from "./Reauth";
import { SettingsHeader, SettingsSubhead } from "./Settings";

export function SiteChanges() {
  const repo = useRepo();
  return (
    <>
      <SettingsHeader title="Site changes">
        Did a change to your site help it in Google? Connect your site&apos;s GitHub repo and Byname finds each change you merge, waits until Google sees the changed
        page, then compares its clicks before and after with comparison pages: a few steady pages you promise not to edit.
      </SettingsHeader>
      <p className="-mt-3 mb-6 text-sm text-muted">
        No GitHub? Log changes by hand under{" "}
        <Link to="/google/site-changes" className="text-fg underline underline-offset-2">
          Google search, Site changes
        </Link>
        .
      </p>
      <div className="space-y-10">
        {repo.isPending ? <Pending /> : repo.isError ? <ErrorNote error={repo.error} /> : <Repo r={repo.data} />}
        <ReadSettings />
      </div>
    </>
  );
}

function Repo({ r }: { r: RepoStatus }) {
  return (
    <>
      {r.repo && <Status r={r} />}
      <section>
        {r.connected ? (
          <details className="text-sm">
            <summary className="cursor-pointer py-3.5 font-medium text-muted hover:text-fg md:py-0">Replace the token or connect another repo</summary>
            <p className="mt-3 mb-3 text-sm text-muted">The same repo keeps its path rules. Another repo starts over with none.</p>
            <ConnectForm initialRepo={r.repo} label="Replace" />
          </details>
        ) : (
          <>
            <SettingsSubhead>{r.repo ? "Reconnect GitHub" : "Connect GitHub"}</SettingsSubhead>
            <ConnectForm initialRepo={r.repo} label="Connect repo" />
          </>
        )}
      </section>
      {r.repo && <Rules key={r.repo} r={r} />}
    </>
  );
}

function Status({ r }: { r: RepoStatus }) {
  const qc = useQueryClient();
  const zone = useZone();
  const set = (st: RepoStatus) => qc.setQueryData(keys.repo, st);
  const test = useMutation({ mutationFn: api.repo.test, onSuccess: set });
  const disconnect = useMutation({ mutationFn: api.repo.disconnect, onSuccess: () => qc.invalidateQueries({ queryKey: keys.repo }) });
  const rows: [string, ReactNode][] = [
    [
      "Repo",
      <span className="flex flex-wrap items-center gap-2">
        <a href={`https://github.com/${r.repo}`} target="_blank" rel="noreferrer" className="font-medium [overflow-wrap:anywhere] hover:underline">
          {r.repo}
        </a>
        {r.connected ? <Badge tone="success">Connected</Badge> : <Badge tone="outline">Disconnected</Badge>}
      </span>,
    ],
    ["Branch", r.default_branch ? <span className="font-mono text-xs">{r.default_branch}</span> : <span className="text-muted">Unknown</span>],
    [
      "Token expires",
      !r.connected ? (
        <span className="text-muted">No token stored</span>
      ) : r.token_expires_at ? (
        <span className="flex flex-wrap items-center gap-2">
          {zone.date(r.token_expires_at)}
          {r.expiring_soon && <Badge tone="warn">Expires soon</Badge>}
        </span>
      ) : (
        <span className="text-muted">Never</span>
      ),
    ],
    [
      "Last read",
      r.last_poll_at ? <RelativeTime iso={r.last_poll_at} /> : <span className="text-muted">{r.connected ? "Not yet. Byname first reads the repo within the hour." : "Never"}</span>,
    ],
    ...(r.last_poll_error ? [["Last error", <span className="text-danger [overflow-wrap:anywhere]">{r.last_poll_error}</span>] as [string, ReactNode]] : []),
  ];
  return (
    <section>
      {r.connected && r.expiring_soon && (
        <p role="status" className="mb-3 flex items-start gap-2 rounded-surface bg-warn-soft px-3 py-2 text-sm text-warn">
          <Icon name="alert" className="mt-0.5 size-4" />
          <span>The token expires soon. Create a new one on GitHub and paste it below, or merges stop being picked up.</span>
        </p>
      )}
      <Card>
        <dl className="divide-y divide-line">
          {rows.map(([k, v]) => (
            <div key={k} className="grid grid-cols-1 gap-0.5 px-4 py-2.5 text-sm sm:grid-cols-[9rem_minmax(0,1fr)] sm:gap-4">
              <dt className="text-xs font-medium text-muted sm:text-sm sm:font-normal">{k}</dt>
              <dd className="min-w-0">{v}</dd>
            </div>
          ))}
        </dl>
      </Card>
      {r.connected && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button onClick={() => test.mutate()} disabled={test.isPending}>
            <Icon name="refresh" className={cx("size-3.5", test.isPending && "animate-spin")} />
            Test
          </Button>
          {test.isSuccess && (
            <span className="flex items-center gap-1 text-xs text-success" aria-live="polite">
              <Icon name="check" className="size-3.5" />
              GitHub answered with this token.
            </span>
          )}
          <ConfirmButton label="Disconnect GitHub" confirmLabel="Disconnect?" onConfirm={() => disconnect.mutate()} disabled={disconnect.isPending} className="ml-auto">
            <Icon name="x" className="size-3.5" />
            Disconnect
          </ConfirmButton>
        </div>
      )}
      <div className="mt-2 space-y-2 empty:hidden">
        <ErrorNote error={test.error} />
        <ErrorNote error={disconnect.error} />
      </div>
      {r.connected && <p className="mt-2 text-xs text-muted">Disconnecting deletes the token, and Byname stops reading the repo every hour. The repo, its rules and the changes found stay.</p>}
    </section>
  );
}

/** owner/name, from what people paste: a full GitHub URL works too. */
const repoName = (raw: string) =>
  raw
    .trim()
    .replace(/^https?:\/\/github\.com\//, "")
    .replace(/\.git$/, "")
    .replace(/\/+$/, "");
const repoPattern = /^[A-Za-z0-9][A-Za-z0-9-]{0,38}\/[A-Za-z0-9._-]{1,100}$/;

function ConnectForm({ initialRepo, label }: { initialRepo: string; label: string }) {
  const qc = useQueryClient();
  const [repo, setRepo] = useState(initialRepo);
  const [token, setToken] = useState("");
  const connect = useReauthMutation({
    mutationFn: (v: { repo: string; token: string }) => api.repo.connect(v.repo, v.token),
    onSuccess: (st) => {
      setToken("");
      qc.setQueryData(keys.repo, st);
      void qc.invalidateQueries({ queryKey: keys.changes });
    },
  });
  const name = repoName(repo);
  const valid = repoPattern.test(name) && token.trim() !== "";
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (valid) connect.mutate({ repo: name, token: token.trim() });
  };
  return (
    <form onSubmit={submit} className="space-y-4">
      <Field label="Repository" hint="owner/name, such as northwind/site.">
        <input className={inputClass} value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="northwind/site" autoCapitalize="none" spellCheck={false} />
      </Field>
      <Field
        label="Token"
        hint={
          <>
            A{" "}
            <a href="https://github.com/settings/personal-access-tokens/new" target="_blank" rel="noreferrer" className="underline hover:text-fg">
              fine-grained personal access token
            </a>{" "}
            for this repo only, with read-only access to Contents and Pull requests. Byname stores it encrypted and never shows it again.
          </>
        }
      >
        <input className={inputClass} type="password" autoComplete="off" spellCheck={false} value={token} onChange={(e) => setToken(e.target.value)} placeholder="github_pat_…" />
      </Field>
      <Button type="submit" variant="primary" disabled={!valid || connect.isPending}>
        <Icon name="github" className="size-3.5" />
        {connect.isPending ? "Checking with GitHub…" : label}
      </Button>
      <ErrorNote error={connect.error} />
    </form>
  );
}

/** internal/changes globRegexp: * within a segment, ** across any number, one group per wildcard. */
function globRegExp(glob: string): RegExp {
  let src = "^";
  for (let i = 0; i < glob.length; ) {
    if (glob.startsWith("**/", i)) {
      src += "(?:(.*)/)?";
      i += 3;
    } else if (glob.startsWith("**", i)) {
      src += "(.*)";
      i += 2;
    } else if (glob[i] === "*") {
      src += "([^/]*)";
      i++;
    } else {
      let j = i;
      while (j < glob.length && glob[j] !== "*") j++;
      src += glob.slice(i, j).replace(/[.+?^${}()|[\]\\]/g, "\\$&");
      i = j;
    }
  }
  return new RegExp(`${src}$`);
}

/** The page the first matching rule gives a repo path, as the poll would map it. */
function pageFor(rules: PathRule[], path: string): { url: string; rule: number } | null {
  for (const [i, r] of rules.entries()) {
    const glob = r.glob.trim();
    if (!glob) continue;
    const m = globRegExp(glob).exec(path);
    if (!m) continue;
    const page = r.url.trim().replace(/\{(\d+)\}/g, (_, n: string) => m[Number(n)] ?? "");
    const cut = page.indexOf("://");
    const url = cut < 0 ? page : page.slice(0, cut + 3) + page.slice(cut + 3).replace(/\/{2,}/g, "/");
    return { url, rule: i + 1 };
  }
  return null;
}

type Row = PathRule & { key: number };

function Rules({ r }: { r: RepoStatus }) {
  const qc = useQueryClient();
  const settings = useSettings();
  const domain = settings.data?.brand.domains[0] || "www.example.com";
  const [next, setNext] = useState(r.rules.length);
  const [added, setAdded] = useState<number>();
  const [rows, setRows] = useState<Row[]>(() => r.rules.map((rule, key) => ({ ...rule, key })));
  const [path, setPath] = useState("");
  const save = useMutation({
    mutationFn: (rules: PathRule[]) => api.repo.putRules(rules),
    onSuccess: (st) => qc.setQueryData(keys.repo, st),
  });
  const rules = rows.map(({ glob, url }) => ({ glob: glob.trim(), url: url.trim() }));
  const dirty = JSON.stringify(rules) !== JSON.stringify(r.rules);
  const complete = rows.every((x) => x.glob.trim() && x.url.trim());
  const edit = (key: number, patch: Partial<PathRule>) => {
    save.reset();
    setRows(rows.map((x) => (x.key === key ? { ...x, ...patch } : x)));
  };
  const move = (i: number, by: -1 | 1) => {
    save.reset();
    const out = [...rows];
    [out[i], out[i + by]] = [out[i + by], out[i]];
    setRows(out);
  };
  const add = () => {
    save.reset();
    setRows([{ glob: "", url: "", key: next }, ...rows]);
    setAdded(next);
    setNext(next + 1);
  };
  const preview = path.trim() ? pageFor(rows, path.trim().replace(/^\/+/, "")) : undefined;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (complete) save.mutate(rules);
  };

  return (
    <section>
      <SettingsSubhead>Path rules</SettingsSubhead>
      <div className="mb-4 space-y-1.5 text-sm text-muted">
        <p>Each rule turns a file in the repo into the page on your site it builds, so Byname knows which page a change touched. The first rule a file matches wins; a file no rule matches is ignored. After you save changed rules, the next hourly read goes through the last 30 days of merges again with them.</p>
        <ul className="list-disc space-y-1 pl-5 text-xs">
          <li>
            <code className="font-mono text-fg">*</code> matches within one folder: <code className="font-mono">content/blog/*.mdx</code> matches <code className="font-mono">content/blog/launch.mdx</code>.
          </li>
          <li>
            <code className="font-mono text-fg">**</code> matches any number of folders, including none: <code className="font-mono">app/**/page.tsx</code> matches <code className="font-mono">app/page.tsx</code> and{" "}
            <code className="font-mono">app/pricing/page.tsx</code>.
          </li>
          <li>
            <code className="font-mono text-fg">{"{1}"}</code>, <code className="font-mono text-fg">{"{2}"}</code> in the URL are what the first and second wildcard matched: <code className="font-mono [overflow-wrap:anywhere]">https://{domain}/blog/{"{1}"}</code> gives{" "}
            <code className="font-mono [overflow-wrap:anywhere]">https://{domain}/blog/launch</code>.
          </li>
        </ul>
      </div>
      <form onSubmit={submit} className="space-y-3">
        <Button onClick={add} disabled={rows.length >= 50}>
          <Icon name="plus" className="size-3.5" />
          Add rule
        </Button>
        {rows.length === 0 ? (
          <p className="rounded-surface border border-dashed border-line-strong px-4 py-6 text-center text-sm text-muted">No rules yet, so no merge maps to a page. Add one for each kind of page your site builds from files.</p>
        ) : (
          <ol aria-label="Path rules" className="space-y-2">
            {rows.map((x, i) => (
              <li key={x.key} className="flex items-start gap-2 rounded-surface border border-line bg-surface p-2">
                <span className="mt-1.5 w-5 shrink-0 text-center text-xs text-faint tabular-nums">{i + 1}</span>
                <div className="grid min-w-0 flex-1 grid-cols-1 gap-2 md:grid-cols-2">
                  <input aria-label={`Rule ${i + 1} glob`} ref={(el) => void (x.key === added && el?.focus({ preventScroll: true }))} className={cx(inputClass, "font-mono text-[16px] md:text-xs")} value={x.glob} onChange={(e) => edit(x.key, { glob: e.target.value })} placeholder="content/blog/*.mdx" autoCapitalize="none" spellCheck={false} />
                  <input aria-label={`Rule ${i + 1} URL`} className={cx(inputClass, "font-mono text-[16px] md:text-xs")} value={x.url} onChange={(e) => edit(x.key, { url: e.target.value })} placeholder={`https://${domain}/blog/{1}`} autoCapitalize="none" spellCheck={false} />
                </div>
                <div className="flex shrink-0 flex-col items-center sm:flex-row">
                  <Button size="sm" variant="ghost" icon aria-label={`Move rule ${i + 1} up`} disabled={i === 0} onClick={() => move(i, -1)}>
                    <Icon name="up" className="size-3.5" />
                  </Button>
                  <Button size="sm" variant="ghost" icon aria-label={`Move rule ${i + 1} down`} disabled={i === rows.length - 1} onClick={() => move(i, 1)}>
                    <Icon name="down" className="size-3.5" />
                  </Button>
                  <Button size="sm" variant="ghost" icon aria-label={`Remove rule ${i + 1}`} onClick={() => (save.reset(), setRows(rows.filter((y) => y.key !== x.key)))}>
                    <Icon name="x" className="size-3.5" />
                  </Button>
                </div>
              </li>
            ))}
          </ol>
        )}
        {rows.length > 0 && (
          <div className="rounded-surface border border-line bg-header p-3">
            <Field label="Try a repo path" hint="Shows the page the rules above give it, saved or not.">
              <input className={cx(inputClass, "font-mono text-[16px] md:text-xs")} value={path} onChange={(e) => setPath(e.target.value)} placeholder="content/blog/launch.mdx" autoCapitalize="none" spellCheck={false} />
            </Field>
            {preview !== undefined && (
              <p className="mt-2 text-xs [overflow-wrap:anywhere]" aria-live="polite">
                {preview ? (
                  <>
                    Rule {preview.rule} gives <span className="font-mono text-fg">{preview.url}</span>
                  </>
                ) : (
                  <span className="text-muted">No rule matches, so a change to this file is ignored.</span>
                )}
              </p>
            )}
          </div>
        )}
        <div className="flex flex-wrap items-center justify-end gap-2">
          <SubmitButton pending={save.isPending} saved={save.isSuccess && !dirty} label="Save rules" disabled={!dirty || !complete} />
        </div>
        <ErrorNote error={save.error} />
      </form>
    </section>
  );
}

type ReadKey = keyof ChangeSettings;
const readKeys: ReadKey[] = ["interim_days", "verdict_days", "position", "ctr_points", "impressions_ratio"];

const readLabels: Record<ReadKey, string> = {
  interim_days: "First result",
  verdict_days: "Final result",
  position: "Average position",
  ctr_points: "Click rate",
  impressions_ratio: "Appearances",
};

const readHints: Record<ReadKey, string> = {
  interim_days: "An early look, marked as early.",
  verdict_days: "Whether the change helped. Later than the first result.",
  position: "Your average spot in Google results, 1 is the top. Up or down by this much.",
  ctr_points: "Clicks divided by appearances. Up or down by this much.",
  impressions_ratio: "Times the page appeared in Google results. Up by this much, or down by its inverse.",
};

function ReadSettings() {
  const s = useChangeSettings();
  return (
    <section>
      <SettingsSubhead>When results come, and what counts</SettingsSubhead>
      <p className="mb-4 text-sm text-muted">
        Each change gets a first result and a final result, counted in days from when Google saw the changed page. A number counts as moved only when the page moves this
        much more than the comparison pages. Changes Google already saw keep their days; new results use the new amounts.
      </p>
      {s.isPending ? <Pending /> : s.isError ? <ErrorNote error={s.error} /> : <ReadSettingsForm initial={s.data} />}
    </section>
  );
}

function ReadSettingsForm({ initial }: { initial: ChangeSettings }) {
  const qc = useQueryClient();
  const [values, setValues] = useState(() => Object.fromEntries(readKeys.map((k) => [k, formatNumber(initial[k])])) as Record<ReadKey, string>);
  const [tried, setTried] = useState(false);
  const save = useMutation({
    mutationFn: (body: ChangeSettings) => api.changes.putSettings(body),
    onSuccess: (st) => {
      qc.setQueryData(keys.changeSettings, st);
      void qc.invalidateQueries({ queryKey: keys.changeList });
    },
  });
  const body = { ...initial };
  const errors: Partial<Record<ReadKey, string>> = {};
  for (const k of readKeys) {
    const p = parseNumber(k, values[k]);
    if ("error" in p) errors[k] = p.error;
    else body[k] = p.value ?? initial[k];
  }
  if (!errors.interim_days && !errors.verdict_days && body.verdict_days <= body.interim_days) errors.verdict_days = "Must be later than the first result.";
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (Object.keys(errors).length === 0) save.mutate(body);
  };
  return (
    <form onSubmit={submit} noValidate className="space-y-5">
      <div className="grid gap-5 sm:grid-cols-2">
        {readKeys.map((k) => (
          <NumberField
            key={k}
            name={k}
            value={values[k]}
            onChange={(val) => {
              save.reset();
              setValues({ ...values, [k]: val });
            }}
            error={tried || values[k] !== formatNumber(initial[k]) ? errors[k] : undefined}
            label={readLabels[k]}
            hint={readHints[k]}
          />
        ))}
      </div>
      <div className="flex flex-wrap items-center justify-end gap-2">
        <SubmitButton pending={save.isPending} saved={save.isSuccess} />
      </div>
      <ErrorNote error={save.error} />
    </form>
  );
}
