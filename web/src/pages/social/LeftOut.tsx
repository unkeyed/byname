import { useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type LeftOut, type SocialNetwork } from "../../api";
import { FieldNote, NoteSlot } from "../../config/forms";
import { keys, useLeftOut } from "../../queries";
import { Button, ConfirmButton, cx, ErrorNote, errorMessage, Field, Icon, inputClass, Pending, selectClass } from "../../ui";
import { NetworkMark, networkOrder, networks } from "./model";

const ours = "Our accounts";

/**
 * The people left out of social, grouped by who they are, and the form that
 * adds one. Onboarding's social step and Settings > Social both show it.
 * empty is shown when nobody is left out yet.
 */
export function LeftOutEditor({ canEdit, brand = "", empty }: { canEdit: boolean; brand?: string; empty?: ReactNode }) {
  const qc = useQueryClient();
  const list = useLeftOut();
  const remove = useMutation({ mutationFn: api.social.removeLeftOut, onSuccess: () => qc.invalidateQueries({ queryKey: keys.social }) });
  if (list.isPending) return <Pending />;
  if (list.isError) return <ErrorNote error={list.error} />;
  const groups = new Map<string, LeftOut[]>();
  for (const l of list.data) groups.set(l.label, [...(groups.get(l.label) ?? []), l]);
  return (
    <div className="space-y-3">
      {groups.size === 0 ? (
        empty
      ) : (
        <ul aria-label="People left out" className="divide-y divide-line rounded-surface border border-line">
          {[...groups].map(([label, accounts]) => (
            <li key={label} className="px-3 py-2.5">
              <div className="text-sm font-medium [overflow-wrap:anywhere]">{label}</div>
              <ul className="mt-1.5 flex flex-wrap gap-2">
                {accounts.map((a) => (
                  <li key={a.id} className="flex items-center gap-1.5 rounded-control border border-line py-0.5 pr-1 pl-0.5 text-xs">
                    <NetworkMark network={a.network} className="size-5" />
                    <a href={a.profile} target="_blank" rel="noreferrer" className="inline-flex min-h-11 min-w-11 items-center [overflow-wrap:anywhere] hover:underline md:min-h-0 md:min-w-0">
                      {a.handle}
                    </a>
                    {a.mentions > 0 && <span className="text-faint tabular-nums">{a.mentions === 1 ? "1 post hidden" : `${a.mentions} posts hidden`}</span>}
                    {canEdit && (
                      <ConfirmButton label={`Stop leaving out ${a.handle} on ${networks[a.network].title}`} confirmLabel="Remove?" onConfirm={() => remove.mutate(a.id)} disabled={remove.isPending}>
                        <Icon name="x" className="size-3" />
                      </ConfirmButton>
                    )}
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ul>
      )}
      <ErrorNote error={remove.error} />
      {canEdit && <AddLeftOut brand={brand} labels={[...groups.keys()]} />}
    </div>
  );
}

/** Starts on "Our accounts" until someone is left out, since the company's own accounts come first. */
function AddLeftOut({ brand, labels }: { brand: string; labels: string[] }) {
  const qc = useQueryClient();
  const [label, setLabel] = useState(labels.length ? "" : ours);
  const [network, setNetwork] = useState<SocialNetwork>("x");
  const [handle, setHandle] = useState("");
  const [tried, setTried] = useState(false);
  const add = useMutation({
    mutationFn: () => api.social.leaveOut({ network, handle: handle.trim(), label: label.trim() }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: keys.social });
      setHandle("");
      setTried(false);
    },
  });
  const missing = !label.trim() ? "Say who it is, such as Our accounts or a teammate's name." : !handle.trim() ? "Give the handle, or paste a link to the profile." : null;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (!missing) add.mutate();
  };
  const slug = brand.toLowerCase().replace(/\s+/g, "");
  const hint = network === "youtube" ? "Channel name or link" : network === "bluesky" ? `${slug || "name"}.bsky.social` : `@${slug || "handle"} or a link`;
  return (
    <form onSubmit={submit} noValidate className="rounded-surface border border-line p-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_9rem_minmax(0,1fr)_auto] sm:items-end">
        <Field label="Who">
          <input className={inputClass} list="left-out-labels" value={label} maxLength={80} onChange={(e) => setLabel(e.target.value)} placeholder="Our accounts, or a teammate's name" />
          <datalist id="left-out-labels">
            {[ours, ...labels.filter((l) => l !== ours)].map((l) => (
              <option key={l} value={l} />
            ))}
          </datalist>
        </Field>
        <Field label="Network">
          <select className={cx(selectClass, "w-full")} value={network} onChange={(e) => setNetwork(e.target.value as SocialNetwork)}>
            {networkOrder.map((n) => (
              <option key={n} value={n}>
                {networks[n].title}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Handle or profile link">
          <input className={inputClass} value={handle} autoCapitalize="none" spellCheck={false} onChange={(e) => setHandle(e.target.value)} placeholder={hint} />
        </Field>
        <Button type="submit" disabled={add.isPending}>
          <Icon name="plus" className="size-3.5" />
          Leave out
        </Button>
      </div>
      <NoteSlot>{tried && missing ? <FieldNote tone="error">{missing}</FieldNote> : add.error ? <FieldNote tone="error">{errorMessage(add.error)}</FieldNote> : null}</NoteSlot>
      <p className="text-xs text-muted">Give one person several handles by using the same name. A YouTube channel is matched by its name as YouTube search shows it.</p>
    </form>
  );
}
