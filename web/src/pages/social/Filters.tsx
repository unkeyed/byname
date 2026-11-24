import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type SocialFilter, type SocialFilterInput, type SocialFilterKind, type SocialFilterMode } from "../../api";
import { FieldNote, NoteSlot } from "../../config/forms";
import { keys, useSocialFilters } from "../../queries";
import { Button, ConfirmButton, cx, EmptyState, ErrorNote, errorMessage, Field, Icon, inputClass, Pending, selectClass } from "../../ui";

/** Adds a filter, and refreshes the social reads it changes. */
export function useAddFilter(onAdded?: () => void) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: SocialFilterInput) => api.social.addFilter(body),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: keys.social });
      onAdded?.();
    },
  });
}

/** "weather", or "r/weather" for a subreddit. */
export const filterName = (kind: SocialFilterKind, value: string) => (kind === "subreddit" ? `r/${value}` : value);

const groups: { kind: SocialFilterKind; mode: SocialFilterMode; title: string; note: string }[] = [
  { kind: "topic", mode: "leave_out", title: "Topics left out", note: "Posts that mention one are hidden." },
  { kind: "topic", mode: "only", title: "Only these topics", note: "Posts that mention none of them are hidden." },
  { kind: "subreddit", mode: "leave_out", title: "Subreddits left out", note: "Reddit posts in them are hidden." },
  { kind: "subreddit", mode: "only", title: "Only these subreddits", note: "Reddit posts in other subreddits are hidden." },
];

/** The topics and subreddits that Social hides or keeps to, and the form that adds one. */
export function FiltersEditor({ canEdit }: { canEdit: boolean }) {
  const qc = useQueryClient();
  const list = useSocialFilters();
  const remove = useMutation({ mutationFn: api.social.removeFilter, onSuccess: () => qc.invalidateQueries({ queryKey: keys.social }) });
  if (list.isPending) return <Pending />;
  if (list.isError) return <ErrorNote error={list.error} />;
  const shown = groups.map((g) => ({ ...g, filters: list.data.filter((f) => f.kind === g.kind && f.mode === g.mode) })).filter((g) => g.filters.length > 0);
  return (
    <div className="space-y-3">
      {shown.length === 0 ? (
        <EmptyState icon="search" title="No filters">
          {canEdit ? "Every post that names you shows. Leave out a topic or subreddit that isn't about you, or keep to the ones that are." : "An admin or editor can add filters here."}
        </EmptyState>
      ) : (
        <ul aria-label="Filters" className="divide-y divide-line rounded-surface border border-line">
          {shown.map((g) => (
            <li key={g.kind + g.mode} className="px-3 py-2.5">
              <div className="text-sm font-medium">{g.title}</div>
              <div className="text-xs text-muted">{g.note}</div>
              <ul className="mt-1.5 flex flex-wrap gap-2">
                {g.filters.map((f) => (
                  <FilterChip key={f.id} f={f} canEdit={canEdit} onRemove={() => remove.mutate(f.id)} removing={remove.isPending} />
                ))}
              </ul>
            </li>
          ))}
        </ul>
      )}
      <ErrorNote error={remove.error} />
      {canEdit && <AddFilter />}
    </div>
  );
}

function FilterChip({ f, canEdit, onRemove, removing }: { f: SocialFilter; canEdit: boolean; onRemove: () => void; removing: boolean }) {
  const name = filterName(f.kind, f.value);
  return (
    <li className="flex items-center gap-1.5 rounded-control border border-line py-0.5 pr-1 pl-2 text-xs">
      <span className="[overflow-wrap:anywhere]">{name}</span>
      <span className="text-faint tabular-nums">{f.mentions === 1 ? "1 post" : `${f.mentions} posts`}</span>
      {canEdit && (
        <ConfirmButton label={`Remove the filter ${name}`} confirmLabel="Remove?" onConfirm={onRemove} disabled={removing}>
          <Icon name="x" className="size-3" />
        </ConfirmButton>
      )}
    </li>
  );
}

function AddFilter() {
  const [kind, setKind] = useState<SocialFilterKind>("topic");
  const [mode, setMode] = useState<SocialFilterMode>("leave_out");
  const [value, setValue] = useState("");
  const [tried, setTried] = useState(false);
  const add = useAddFilter(() => {
    setValue("");
    setTried(false);
  });
  const missing = value.trim() ? null : kind === "topic" ? "Give a word or phrase." : "Give the subreddit's name, or paste a link to it.";
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (!missing) add.mutate({ kind, mode, value: value.trim() });
  };
  return (
    <form onSubmit={submit} noValidate className="rounded-surface border border-line p-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-[8rem_10rem_minmax(0,1fr)_auto] sm:items-end">
        <Field label="Filter by">
          <select className={cx(selectClass, "w-full")} value={kind} onChange={(e) => setKind(e.target.value as SocialFilterKind)}>
            <option value="topic">Topic</option>
            <option value="subreddit">Subreddit</option>
          </select>
        </Field>
        <Field label="To">
          <select className={cx(selectClass, "w-full")} value={mode} onChange={(e) => setMode(e.target.value as SocialFilterMode)}>
            <option value="leave_out">Leave it out</option>
            <option value="only">Show only these</option>
          </select>
        </Field>
        <Field label={kind === "topic" ? "Word or phrase" : "Subreddit or link"}>
          <input
            className={inputClass}
            value={value}
            maxLength={100}
            autoCapitalize="none"
            spellCheck={false}
            onChange={(e) => setValue(e.target.value)}
            placeholder={kind === "topic" ? "weather" : "r/weather"}
          />
        </Field>
        <Button type="submit" disabled={add.isPending}>
          <Icon name="plus" className="size-3.5" />
          Add
        </Button>
      </div>
      <NoteSlot>{tried && missing ? <FieldNote tone="error">{missing}</FieldNote> : add.error ? <FieldNote tone="error">{errorMessage(add.error)}</FieldNote> : null}</NoteSlot>
      <p className="text-xs text-muted">
        {kind === "topic"
          ? "A topic matches as a whole word in any case, so weather matches “Weather” and “#weather” but not “weatherproof”."
          : "Give the subreddit's name, r/name or a link to it. Posts on other networks are not affected."}
      </p>
    </form>
  );
}
