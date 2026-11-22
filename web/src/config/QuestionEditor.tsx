import { useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Question, type QuestionSet, type QuestionsInput } from "../api";
import { absoluteTime } from "../format";
import { keys, useCan, useQuestions, useSettings } from "../queries";
import { Badge, Button, Card, CardHeader, cx, Dialog, DialogBody, EmptyState, ErrorNote, Field, Icon, inputClass, labelClass, Pending, RelativeTime, selectClass } from "../ui";
import { defaultFooter, FieldNote, ReadOnlyNote, SubmitButton, type Footer } from "./forms";
import { dedupe, namesIn, normalizeQuestion, questionError, questionLength, questionsFromCSV, questionsFromLines, type DraftQuestion } from "./rules";

/** A question being edited. from is the current question it was loaded from, which it carries on by default if its text changes. */
interface Row extends DraftQuestion {
  key: number;
  from?: number;
}

let nextKey = 1;
const rowsOf = (set: QuestionSet | null): Row[] => (set?.questions ?? []).map((q) => ({ key: nextKey++, from: q.id, group: q.group, text: q.text }));
const clean = (s: string) => s.trim().replace(/\s+/g, " ");

/** Where a changed or removed current question goes: the row that carries it on, or retired. */
type Carry = Record<number, number | "retire">;

const modes = [
  { id: "one", label: "One by one" },
  { id: "list", label: "Paste a list" },
  { id: "csv", label: "CSV" },
] as const;
type Mode = (typeof modes)[number]["id"];

/**
 * The question set: onboarding step 3 and Settings > Questions. Saving
 * makes a new version; when current questions changed or went, a dialog
 * maps each to the question that carries it on, or retires it.
 */
export function QuestionEditor({ footer = defaultFooter, onSaved, submitLabel, history }: { footer?: Footer; onSaved?: () => unknown; submitLabel?: string; history?: boolean }) {
  const current = useQuestions();
  const settings = useSettings();
  if (current.isPending || settings.isPending) return <Pending />;
  if (current.isError || settings.isError) return <ErrorNote error={current.error ?? settings.error} />;
  const names = [settings.data.brand.name, ...settings.data.brand.aliases].filter(Boolean);
  return (
    <>
      <EditorBody key={current.data?.id ?? 0} current={current.data} names={names} footer={footer} onSaved={onSaved} submitLabel={submitLabel} history={history} />
      {history && <Versions />}
    </>
  );
}

function EditorBody({
  current,
  names,
  footer,
  onSaved,
  submitLabel,
  history,
}: {
  current: QuestionSet | null;
  names: string[];
  footer: Footer;
  onSaved?: () => unknown;
  submitLabel?: string;
  history?: boolean;
}) {
  const qc = useQueryClient();
  const canEdit = useCan("config:write");
  const [rows, setRows] = useState<Row[]>(() => rowsOf(current));
  const [notes, setNotes] = useState("");
  const [carry, setCarry] = useState<Carry | null>(null);
  const [tried, setTried] = useState(false);
  const groups = [...new Set(rows.map((r) => r.group))];

  const save = useMutation({
    mutationFn: (body: QuestionsInput) => api.questions.put(body),
    onSuccess: async (set) => {
      setCarry(null);
      qc.setQueryData(keys.questionSet, set);
      void qc.invalidateQueries({ queryKey: keys.questionSets });
      void qc.invalidateQueries({ queryKey: keys.estimate });
      await onSaved?.();
    },
  });

  const byText = new Map(rows.map((r) => [normalizeQuestion(r.text), r.key]));
  const auto = (current?.questions ?? []).filter((q) => byText.has(normalizeQuestion(q.text)));
  const unmapped = (current?.questions ?? []).filter((q) => !byText.has(normalizeQuestion(q.text)));
  const invalid = rows.filter((r) => questionError(r.group, r.text));

  const send = (c: Carry) =>
    save.mutate({
      notes: notes.trim(),
      questions: rows.map((r) => ({
        group: clean(r.group),
        text: clean(r.text),
        replaces: Object.entries(c)
          .filter(([, to]) => to === r.key)
          .map(([from]) => Number(from)),
      })),
      retired: Object.entries(c)
        .filter(([, to]) => to === "retire")
        .map(([from]) => Number(from)),
    });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (rows.length === 0 || invalid.length > 0) return;
    if (unmapped.length === 0) return send({});
    // An edited question carries on the one it was loaded from, unless that row is gone.
    setCarry(Object.fromEntries(unmapped.map((q) => [q.id, rows.find((r) => r.from === q.id)?.key ?? "retire"])));
  };

  const add = (incoming: DraftQuestion[]) => {
    const { added, dropped } = dedupe(rows, incoming);
    setRows([...rows, ...added.map((q) => ({ ...q, group: clean(q.group), text: clean(q.text), key: nextKey++ }))]);
    return { added, dropped };
  };

  return (
    <div className="space-y-5">
      {!canEdit && <ReadOnlyNote />}
      {canEdit && <AddQuestions groups={groups} onAdd={add} names={names} />}

      <QuestionList rows={rows} names={names} canEdit={canEdit} onChange={setRows} tried={tried} />

      <form onSubmit={submit} noValidate className="space-y-5">
      {canEdit && history && current && (
        <Field label="Notes for this version" hint="Why the set changed, shown in the version history.">
          <input className={inputClass} value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="Added three pricing questions" />
        </Field>
      )}
      {tried && rows.length === 0 && <FieldNote tone="error">Add at least one question.</FieldNote>}
      {tried && invalid.length > 0 && <FieldNote tone="error">Fix the {invalid.length === 1 ? "question" : `${invalid.length} questions`} marked above first.</FieldNote>}
      {canEdit &&
        footer(
          <SubmitButton
            pending={save.isPending}
            saved={save.isSuccess}
            label={submitLabel ?? (current ? `Save as version ${current.version + 1}` : "Save questions")}
          />,
        )}
      {!carry && <ErrorNote error={save.error} />}

      <Dialog open={carry !== null} onClose={() => setCarry(null)} title={`Save version ${(current?.version ?? 0) + 1}`} wide>
        {carry && current && (
          <CarryDialog
            unmapped={unmapped}
            auto={auto}
            rows={rows}
            carry={carry}
            onChange={setCarry}
            onConfirm={() => send(carry)}
            pending={save.isPending}
            error={save.error}
          />
        )}
      </Dialog>
      </form>
    </div>
  );
}

function AddQuestions({ groups, onAdd, names }: { groups: string[]; onAdd: (qs: DraftQuestion[]) => { added: DraftQuestion[]; dropped: DraftQuestion[] }; names: string[] }) {
  const [mode, setMode] = useState<Mode>("one");
  const [group, setGroup] = useState(groups[0] ?? "");
  const [text, setText] = useState("");
  const [bulk, setBulk] = useState("");
  const [bulkGroup, setBulkGroup] = useState("General");
  const [report, setReport] = useState<{ added: number; dropped: DraftQuestion[]; rejected: { q: DraftQuestion; why: string }[] } | null>(null);
  const [oneError, setOneError] = useState<string | null>(null);

  const addOne = () => {
    const err = questionError(group, text);
    if (err) return setOneError(err);
    const { added } = onAdd([{ group, text }]);
    if (!added.length) return setOneError("That question is already in the set, ignoring case and punctuation.");
    setOneError(null);
    setText("");
  };
  const addBulk = () => {
    const parsed = mode === "csv" ? questionsFromCSV(bulk, clean(bulkGroup) || "General") : questionsFromLines(bulk, clean(bulkGroup) || "General");
    const rejected = parsed.flatMap((q) => {
      const why = questionError(q.group, q.text);
      return why ? [{ q, why }] : [];
    });
    const { added, dropped } = onAdd(parsed.filter((q) => !questionError(q.group, q.text)));
    setReport({ added: added.length, dropped, rejected });
    if (added.length) setBulk("");
  };
  const readFile = (file: File | undefined) => {
    if (file) void file.text().then(setBulk);
  };
  const flagged = text && namesIn(text, names);

  return (
    <Card>
      <div className="flex flex-wrap items-center gap-2 px-4 pt-3">
        <h3 className="mr-auto text-sm font-semibold">Add questions</h3>
        <div role="tablist" aria-label="How to add" className="flex rounded-control border border-line p-0.5">
          {modes.map((m) => (
            <button
              key={m.id}
              type="button"
              role="tab"
              aria-selected={mode === m.id}
              onClick={() => {
                setMode(m.id);
                setReport(null);
              }}
              className={cx("h-11 min-w-11 rounded-inner px-2 md:h-6 md:min-w-0 text-xs font-medium transition-colors", mode === m.id ? "bg-subtle-strong text-fg" : "text-muted hover:text-fg")}
            >
              {m.label}
            </button>
          ))}
        </div>
      </div>
      <div className="space-y-3 p-4">
        <p className="text-xs text-muted">
          Write them the way a buyer would ask an AI, without your brand name. Byname sends each question exactly as written, so a question that names you
          inflates your results. {questionLength.min} to {questionLength.max} characters each.
        </p>
        {mode === "one" ? (
          <div className="space-y-2">
            <div className="grid gap-2 sm:grid-cols-[12rem_1fr]">
              <Field label="Group">
                <input className={inputClass} list="question-groups" value={group} onChange={(e) => setGroup(e.target.value)} placeholder="Deploying" />
              </Field>
              <Field label="Question">
                <input
                  className={inputClass}
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      addOne();
                    }
                  }}
                  placeholder="What's the easiest way to deploy a Go API?"
                />
              </Field>
            </div>
            <datalist id="question-groups">
              {groups.map((g) => (
                <option key={g} value={g} />
              ))}
            </datalist>
            {oneError && <FieldNote tone="error">{oneError}</FieldNote>}
            {flagged && flagged.length > 0 && <BrandFlag names={flagged} />}
            <div className="flex justify-end">
              <Button onClick={addOne}>
                <Icon name="plus" className="size-3.5" />
                Add question
              </Button>
            </div>
          </div>
        ) : (
          <div className="space-y-2">
            <label className="block">
              <span className={labelClass}>{mode === "csv" ? "CSV with group and text columns. The header row is optional." : "One question per line"}</span>
              <textarea
                className={cx(inputClass, "h-36 py-2 font-mono text-xs leading-relaxed")}
                value={bulk}
                onChange={(e) => setBulk(e.target.value)}
                placeholder={
                  mode === "csv"
                    ? 'group,text\nDeploying,"What\'s the easiest way to deploy a Go API?"\nPricing,"Which hosting platforms have a free tier, and what are the limits?"'
                    : "What's the easiest way to deploy a Go API?\nWhich hosting platforms have a free tier?"
                }
              />
            </label>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
              <div className="sm:w-56">
                <Field label={mode === "csv" ? "Group for rows without one" : "Group for these questions"}>
                  <input className={inputClass} list="question-groups" value={bulkGroup} onChange={(e) => setBulkGroup(e.target.value)} />
                </Field>
              </div>
              {mode === "csv" && (
                <label className="cursor-pointer text-xs text-muted underline-offset-2 hover:text-fg hover:underline sm:mb-2">
                  Or choose a .csv file
                  <input type="file" accept=".csv,text/csv" className="sr-only" onChange={(e) => readFile(e.target.files?.[0])} />
                </label>
              )}
              <Button className="sm:ml-auto" onClick={addBulk} disabled={!bulk.trim()}>
                <Icon name="plus" className="size-3.5" />
                Add questions
              </Button>
            </div>
            {report && <ImportReport {...report} />}
          </div>
        )}
      </div>
    </Card>
  );
}

function ImportReport({ added, dropped, rejected }: { added: number; dropped: DraftQuestion[]; rejected: { q: DraftQuestion; why: string }[] }) {
  return (
    <div className="space-y-1.5 rounded-control bg-subtle px-3 py-2 text-xs" aria-live="polite">
      <p className="font-medium">
        Added {added} {added === 1 ? "question" : "questions"}.
      </p>
      {dropped.length > 0 && (
        <div>
          <p className="text-muted">Dropped {dropped.length} that repeat a question already in the set, ignoring case and punctuation:</p>
          <ul className="mt-0.5 list-disc pl-4 text-muted">
            {dropped.map((q, i) => (
              <li key={i} className="[overflow-wrap:anywhere]">
                {q.text}
              </li>
            ))}
          </ul>
        </div>
      )}
      {rejected.length > 0 && (
        <div>
          <p className="text-danger">Left out {rejected.length} that need fixing:</p>
          <ul className="mt-0.5 list-disc pl-4 text-danger">
            {rejected.map(({ q, why }, i) => (
              <li key={i} className="[overflow-wrap:anywhere]">
                “{q.text || "(empty)"}”: {why}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function BrandFlag({ names }: { names: string[] }) {
  return (
    <FieldNote tone="warn">
      Names {names.map((n) => `“${n}”`).join(" and ")}. Byname sends the question as written, so a question that names you inflates your results. Ask it the way a buyer who
      has not heard of you would.
    </FieldNote>
  );
}

function QuestionList({ rows, names, canEdit, onChange, tried }: { rows: Row[]; names: string[]; canEdit: boolean; onChange: (rows: Row[]) => void; tried: boolean }) {
  const [editing, setEditing] = useState<number | null>(null);
  if (rows.length === 0) {
    return (
      <EmptyState icon="message" title="No questions yet">
        {canEdit ? "Add at least one. Calls and cost per check scale with the number of questions." : "Nobody has added questions yet."}
      </EmptyState>
    );
  }
  const groups = [...new Set(rows.map((r) => r.group))];
  const flaggedCount = rows.filter((r) => namesIn(r.text, names).length > 0).length;
  return (
    <Card>
      <CardHeader
        title={`${rows.length} ${rows.length === 1 ? "question" : "questions"} in ${groups.length} ${groups.length === 1 ? "group" : "groups"}`}
        description={flaggedCount > 0 ? `${flaggedCount} name your brand. Consider rewording them.` : undefined}
      />
      <div className="divide-y divide-line border-t border-line">
        {groups.map((g) => (
          <section key={g} aria-label={g}>
            <h4 className="bg-header px-4 py-1.5 text-xs font-medium text-muted">
              {g} <span className="text-faint">· {rows.filter((r) => r.group === g).length}</span>
            </h4>
            <ul className="divide-y divide-line">
              {rows
                .filter((r) => r.group === g)
                .map((r) =>
                  editing === r.key ? (
                    <RowEditor
                      key={r.key}
                      row={r}
                      others={rows.filter((o) => o.key !== r.key)}
                      onSave={(next) => {
                        onChange(rows.map((o) => (o.key === r.key ? next : o)));
                        setEditing(null);
                      }}
                      onCancel={() => setEditing(null)}
                    />
                  ) : (
                    <QuestionRow
                      key={r.key}
                      row={r}
                      names={names}
                      canEdit={canEdit}
                      showError={tried}
                      onEdit={() => setEditing(r.key)}
                      onRemove={() => onChange(rows.filter((o) => o.key !== r.key))}
                    />
                  ),
                )}
            </ul>
          </section>
        ))}
      </div>
    </Card>
  );
}

function QuestionRow({ row, names, canEdit, showError, onEdit, onRemove }: { row: Row; names: string[]; canEdit: boolean; showError: boolean; onEdit: () => void; onRemove: () => void }) {
  const flagged = namesIn(row.text, names);
  const error = questionError(row.group, row.text);
  return (
    <li className="flex items-start gap-2 px-4 py-2.5">
      <div className="min-w-0 flex-1">
        <p className="text-sm [overflow-wrap:anywhere]">{row.text}</p>
        {flagged.length > 0 && <BrandFlag names={flagged} />}
        {showError && error && <FieldNote tone="error">{error}</FieldNote>}
      </div>
      {canEdit && (
        <div className="-my-0.5 flex shrink-0 items-center">
          <Button size="sm" variant="ghost" onClick={onEdit} aria-label="Edit question" title="Edit" icon>
            <Icon name="pencil" className="size-3.5" />
          </Button>
          <Button size="sm" variant="ghost" onClick={onRemove} aria-label="Remove question" title="Remove" icon>
            <Icon name="x" className="size-3.5" />
          </Button>
        </div>
      )}
    </li>
  );
}

function RowEditor({ row, others, onSave, onCancel }: { row: Row; others: Row[]; onSave: (r: Row) => void; onCancel: () => void }) {
  const [group, setGroup] = useState(row.group);
  const [text, setText] = useState(row.text);
  const [error, setError] = useState<string | null>(null);
  const apply = () => {
    const err = questionError(group, text);
    if (err) return setError(err);
    if (others.some((o) => normalizeQuestion(o.text) === normalizeQuestion(text))) return setError("Another question already says this, ignoring case and punctuation.");
    onSave({ ...row, group: clean(group), text: clean(text) });
  };
  return (
    <li className="space-y-2 bg-header px-4 py-3">
      <div className="grid gap-2 sm:grid-cols-[12rem_1fr]">
        <Field label="Group">
          <input className={inputClass} list="question-groups" value={group} onChange={(e) => setGroup(e.target.value)} />
        </Field>
        <Field label={`Question (${clean(text).length} of ${questionLength.max})`}>
          <textarea className={cx(inputClass, "h-auto min-h-16 py-1.5")} rows={2} value={text} onChange={(e) => setText(e.target.value)} autoFocus />
        </Field>
      </div>
      {error && <FieldNote tone="error">{error}</FieldNote>}
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button size="sm" onClick={apply}>
          Done
        </Button>
      </div>
    </li>
  );
}

function CarryDialog({
  unmapped,
  auto,
  rows,
  carry,
  onChange,
  onConfirm,
  pending,
  error,
}: {
  unmapped: Question[];
  auto: Question[];
  rows: Row[];
  carry: Carry;
  onChange: (c: Carry) => void;
  onConfirm: () => void;
  pending: boolean;
  error: unknown;
}) {
  const autoKeys = new Set(auto.map((q) => normalizeQuestion(q.text)));
  // A changed question most likely went to a new one, not to one that already carries an old question on by its text.
  const targets = [...rows].sort((a, b) => Number(autoKeys.has(normalizeQuestion(a.text))) - Number(autoKeys.has(normalizeQuestion(b.text))));
  return (
    <DialogBody>
      <div className="space-y-4">
        <p className="text-sm text-muted">
          {unmapped.length === 1 ? "One current question changed or was removed." : `${unmapped.length} current questions changed or were removed.`} Pick the new question that
          carries each one on, so its history continues, or retire it.
        </p>
        <ul className="space-y-3">
          {unmapped.map((q) => (
            <li key={q.id} className="rounded-surface border border-line p-3">
              <p className="text-xs text-muted">{q.group}</p>
              <p className="mb-2 text-sm [overflow-wrap:anywhere]">{q.text}</p>
              <select
                aria-label={`Where “${q.text}” goes`}
                className={cx(selectClass, "w-full")}
                value={String(carry[q.id])}
                onChange={(e) => onChange({ ...carry, [q.id]: e.target.value === "retire" ? "retire" : Number(e.target.value) })}
              >
                <option value="retire">Retire it. Its history ends with this version.</option>
                {targets.map((r) => (
                  <option key={r.key} value={r.key}>
                    Carried on by: {r.text.length > 90 ? `${r.text.slice(0, 90)}…` : r.text}
                  </option>
                ))}
              </select>
            </li>
          ))}
        </ul>
        {auto.length > 0 && (
          <details className="text-xs text-muted">
            <summary className="cursor-pointer py-3.5 md:py-0">
              {auto.length} {auto.length === 1 ? "question carries" : "questions carry"} on by themselves (same text, ignoring case and punctuation)
            </summary>
            <ul className="mt-1.5 list-disc space-y-0.5 pl-4">
              {auto.map((q) => (
                <li key={q.id} className="[overflow-wrap:anywhere]">
                  {q.text}
                </li>
              ))}
            </ul>
          </details>
        )}
        <ErrorNote error={error} />
        <div className="flex justify-end">
          <Button variant="primary" onClick={onConfirm} disabled={pending}>
            {pending ? "Saving…" : "Save new version"}
          </Button>
        </div>
      </div>
    </DialogBody>
  );
}

function Versions() {
  const versions = useQuery({ queryKey: keys.questionSets, queryFn: async () => (await api.questions.versions()) ?? [] });
  let body: ReactNode;
  if (versions.isPending) body = <Pending />;
  else if (versions.isError) body = <div className="p-4"><ErrorNote error={versions.error} /></div>;
  else if (versions.data.length === 0) body = <p className="px-4 pb-4 text-sm text-muted">No versions yet. Saving the questions makes version 1.</p>;
  else
    body = (
      <ul className="divide-y divide-line border-t border-line">
        {versions.data.map((v, i) => (
          <li key={v.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-4 py-2.5 text-sm">
            <span className="font-medium">Version {v.version}</span>
            {i === 0 && <Badge tone="success">Current</Badge>}
            <span className="text-muted">
              {v.questions} {v.questions === 1 ? "question" : "questions"}
            </span>
            <span className="text-xs text-muted" title={absoluteTime(v.created_at)}>
              {absoluteTime(v.created_at)} (<RelativeTime iso={v.created_at} />)
            </span>
            {v.notes && <span className="basis-full text-xs text-muted [overflow-wrap:anywhere]">{v.notes}</span>}
          </li>
        ))}
      </ul>
    );
  return (
    <Card className="mt-10">
      <CardHeader title="Version history" description="Each save makes a new version. Results carry over through the map you give, so charts never compare raw totals across versions." />
      {body}
    </Card>
  );
}
