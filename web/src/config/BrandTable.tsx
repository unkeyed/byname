import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type BrandEntry, type BrandInput } from "../api";
import { keys, useBrands, useCan } from "../queries";
import { Badge, Button, Card, ConfirmButton, cx, EmptyState, ErrorNote, Field, Icon, inputClass, PageSpinner, selectClass } from "../ui";
import { FieldNote } from "./forms";
import { commonWordAliases, parseHosts, splitList } from "./rules";

type Kind = BrandInput["kind"];

export const kindInfo: Record<Kind, { label: string; detail: string }> = {
  competitor: { label: "Competitor", detail: "Counted in share of voice and shown beside you." },
  technology: {
    label: "Technology",
    detail: "Something answers name alongside you that nobody buys instead of you, such as Kubernetes or Redis. Stored and shown, never counted in share of voice.",
  },
};

interface Draft {
  name: string;
  aliases: string;
  domains: string;
  kind: Kind;
}

const empty: Draft = { name: "", aliases: "", domains: "", kind: "competitor" };
const toDraft = (b: BrandEntry): Draft => ({ name: b.name, aliases: b.aliases.join(", "), domains: b.domains.join(", "), kind: b.kind === "technology" ? "technology" : "competitor" });

/** Competitors and technologies: onboarding step 2 and Settings > Competitors. Each change saves at once. */
export function BrandTable() {
  const brands = useBrands();
  const canEdit = useCan("config:write");
  const [editing, setEditing] = useState<number | null>(null);
  if (brands.isPending) return <PageSpinner />;
  if (brands.isError) return <ErrorNote error={brands.error} />;
  const all = brands.data;
  const others = all.filter((b) => b.kind !== "self");

  return (
    <div className="space-y-4">
      {canEdit && (
        <Card className="p-4">
          <h3 className="mb-3 text-sm font-semibold">Add a competitor or technology</h3>
          <BrandRowForm all={all} />
        </Card>
      )}
      {others.length === 0 ? (
        <EmptyState icon="users" title="No competitors yet">
          {canEdit ? "Add the brands you compete with. You can also start with none and add brands that the first check's answers name." : "Nobody has added competitors yet."}
        </EmptyState>
      ) : (
        <Card>
          <ul aria-label="Competitors and technologies" className="divide-y divide-line">
            {others.map((b) =>
              editing === b.id ? (
                <li key={b.id} className="px-4 py-3">
                  <BrandRowForm all={all} editing={b} onDone={() => setEditing(null)} />
                </li>
              ) : (
                <BrandRow key={b.id} b={b} canEdit={canEdit} onEdit={() => setEditing(b.id)} />
              ),
            )}
          </ul>
        </Card>
      )}
    </div>
  );
}

function BrandRow({ b, canEdit, onEdit }: { b: BrandEntry; canEdit: boolean; onEdit: () => void }) {
  const qc = useQueryClient();
  const remove = useMutation({ mutationFn: () => api.brands.remove(b.id), onSuccess: () => qc.invalidateQueries({ queryKey: keys.brands }) });
  const kind = b.kind === "technology" ? "technology" : "competitor";
  return (
    <li className="px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="min-w-0 flex-1 basis-48">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium [overflow-wrap:anywhere]">{b.name}</span>
            <Badge tone={kind === "technology" ? "outline" : "neutral"} title={kindInfo[kind].detail}>
              {kindInfo[kind].label}
            </Badge>
          </div>
          <div className="mt-0.5 text-xs text-muted [overflow-wrap:anywhere]">
            {b.domains.length > 0 ? b.domains.join(", ") : "No domain"}
            {b.aliases.length > 0 && <> · also {b.aliases.join(", ")}</>}
          </div>
        </div>
        {canEdit && (
          <div className="ml-auto flex shrink-0 items-center gap-1">
            <Button size="sm" variant="ghost" onClick={onEdit} aria-label={`Edit ${b.name}`}>
              <Icon name="pencil" className="size-3.5" />
              Edit
            </Button>
            <ConfirmButton label={`Delete ${b.name}`} confirmLabel="Delete?" onConfirm={() => remove.mutate()} disabled={remove.isPending}>
              <Icon name="trash" className="size-3.5" />
            </ConfirmButton>
          </div>
        )}
      </div>
      {remove.error && (
        <div className="mt-2">
          <ErrorNote error={remove.error} />
        </div>
      )}
    </li>
  );
}

function BrandRowForm({ all, editing, onDone }: { all: BrandEntry[]; editing?: BrandEntry; onDone?: () => void }) {
  const qc = useQueryClient();
  const [d, setD] = useState<Draft>(editing ? toDraft(editing) : empty);
  const [tried, setTried] = useState(false);
  const set = (patch: Partial<Draft>) => setD({ ...d, ...patch });
  const save = useMutation({
    mutationFn: (body: BrandInput) => (editing ? api.brands.update(editing.id, body) : api.brands.upsert(body)),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: keys.brands });
      setD(empty);
      setTried(false);
      onDone?.();
    },
  });

  const name = d.name.trim().replace(/\s+/g, " ");
  const taken = all.find((b) => b.id !== editing?.id && b.name.toLowerCase() === name.toLowerCase());
  const nameError = !name
    ? "Name is required."
    : name.length > 80
      ? "Names are at most 80 characters."
      : taken
        ? taken.kind === "self"
          ? `${taken.name} is your own brand.`
          : `${taken.name} is already on the list.`
        : null;
  const hosts = parseHosts(d.domains);
  const aliasList = splitList(d.aliases);
  const common = commonWordAliases(aliasList);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (nameError || "error" in hosts) return;
    save.mutate({ name, aliases: aliasList, domains: hosts.hosts, kind: d.kind });
  };

  return (
    <form onSubmit={submit} noValidate className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <Field label="Name">
            <input className={inputClass} value={d.name} onChange={(e) => set({ name: e.target.value })} placeholder="Kestrel" aria-invalid={tried && !!nameError} autoFocus={!!editing} />
          </Field>
          {tried && nameError && <FieldNote tone="error">{nameError}</FieldNote>}
        </div>
        <div>
          <Field label="Domain">
            <input className={inputClass} value={d.domains} onChange={(e) => set({ domains: e.target.value })} placeholder="kestrel.io" autoCapitalize="none" spellCheck={false} />
          </Field>
          {"error" in hosts ? (
            <FieldNote tone="error">{hosts.error}</FieldNote>
          ) : hosts.trimmed.length > 0 ? (
            <FieldNote tone="info">Kept the host only: {hosts.trimmed.join(", ")}.</FieldNote>
          ) : (
            <FieldNote tone="info">Optional. Separate more than one with commas.</FieldNote>
          )}
        </div>
        <div>
          <Field label="Aliases">
            <input className={inputClass} value={d.aliases} onChange={(e) => set({ aliases: e.target.value })} placeholder="Kestrel Cloud, KestrelDB" />
          </Field>
          {common.length > 0 ? (
            <FieldNote tone="warn">
              {common.map((c) => `“${c}”`).join(", ")} {common.length === 1 ? "is an everyday word" : "are everyday words"} and would match answers that are not about{" "}
              {name || "them"}.
            </FieldNote>
          ) : (
            <FieldNote tone="info">Comma separated. Each counts as this brand.</FieldNote>
          )}
        </div>
        <div>
          <Field label="Kind">
            <select className={cx(selectClass, "w-full")} value={d.kind} onChange={(e) => set({ kind: e.target.value as Kind })}>
              {(Object.keys(kindInfo) as Kind[]).map((k) => (
                <option key={k} value={k}>
                  {kindInfo[k].label}
                </option>
              ))}
            </select>
          </Field>
          <FieldNote tone="info">{kindInfo[d.kind].detail}</FieldNote>
        </div>
      </div>
      <ErrorNote error={save.error} />
      <div className="flex justify-end gap-2">
        {onDone && (
          <Button variant="ghost" onClick={onDone}>
            Cancel
          </Button>
        )}
        <Button type="submit" variant={editing ? "primary" : "secondary"} disabled={save.isPending}>
          {editing ? (
            "Save"
          ) : (
            <>
              <Icon name="plus" className="size-3.5" />
              Add
            </>
          )}
        </Button>
      </div>
    </form>
  );
}
