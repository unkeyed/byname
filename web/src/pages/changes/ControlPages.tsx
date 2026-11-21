import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type ControlInput, type ControlPage } from "../../api";
import { keys, useControlPages } from "../../queries";
import { Badge, Button, Card, CardHeader, ConfirmButton, Dialog, DialogBody, EmptyState, ErrorNote, Field, Icon, inputClass, PageSpinner } from "../../ui";
import { fullDay } from "../search/model";

/** The pages verdicts compare against, with add, edit and delete for editors. */
export function ControlPagesCard({ canEdit }: { canEdit: boolean }) {
  const qc = useQueryClient();
  const pages = useControlPages();
  const [editing, setEditing] = useState<ControlPage | "new" | null>(null);
  const remove = useMutation({ mutationFn: api.controlPages.remove, onSuccess: () => qc.invalidateQueries({ queryKey: keys.controlPages }) });
  const active = pages.data?.filter((p) => p.active).length ?? 0;

  let body;
  if (pages.isPending) body = <PageSpinner />;
  else if (pages.isError) body = <div className="px-4 pb-4"><ErrorNote error={pages.error} /></div>;
  else if (pages.data.length === 0)
    body = (
      <div className="px-4 pb-4">
        <EmptyState
          icon="pin"
          title="No control pages"
          action={
            canEdit ? (
              <Button variant="primary" onClick={() => setEditing("new")}>
                <Icon name="plus" className="size-3.5" />
                Add a control page
              </Button>
            ) : undefined
          }
        >
          {canEdit
            ? "Without them every read says there is not enough data. Add a few pages with steady traffic that nobody will edit before the hold date."
            : "Without them every read says there is not enough data. Someone with the Editor or Admin role can add them."}
        </EmptyState>
      </div>
    );
  else
    body = (
      <ul aria-label="Control pages" className="divide-y divide-line border-t border-line">
        {pages.data.map((p) => (
          <li key={p.id} className="flex items-start gap-3 px-4 py-2.5">
            <div className="min-w-0 flex-1">
              <a href={p.url} target="_blank" rel="noreferrer" className="text-sm font-medium [overflow-wrap:anywhere] hover:underline">
                {p.url}
              </a>
              <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
                {p.active ? <Badge tone="success">Active</Badge> : <Badge tone="outline">Ended</Badge>}
                <span>
                  {p.active ? "Held until" : "Held through"} {fullDay(p.hold_until)}
                </span>
                {p.note && <span className="[overflow-wrap:anywhere] text-faint">{p.note}</span>}
              </div>
            </div>
            {canEdit && (
              <div className="-my-0.5 flex shrink-0 items-center">
                <Button size="sm" variant="ghost" onClick={() => setEditing(p)} aria-label={`Edit ${p.url}`} title="Edit" className="w-7 px-0">
                  <Icon name="pencil" className="size-3.5" />
                </Button>
                <ConfirmButton label={`Remove ${p.url}`} confirmLabel="Remove?" onConfirm={() => remove.mutate(p.id)} disabled={remove.isPending}>
                  <Icon name="trash" className="size-3.5" />
                </ConfirmButton>
              </div>
            )}
          </li>
        ))}
      </ul>
    );

  return (
    <Card>
      <CardHeader
        title="Control pages"
        description={
          pages.data && pages.data.length > 0
            ? `${active} of ${pages.data.length} active. Each read compares a changed page with the active ones over the same days; a page drops out after its hold date.`
            : "Pages you leave unchanged, so a read can tell a page's own move from a sitewide one."
        }
      >
        {canEdit && pages.data && pages.data.length > 0 && (
          <Button size="sm" onClick={() => setEditing("new")}>
            <Icon name="plus" className="size-3.5" />
            Add control page
          </Button>
        )}
      </CardHeader>
      {body}
      {remove.error != null && (
        <div className="px-4 pb-4">
          <ErrorNote error={remove.error} />
        </div>
      )}
      <Dialog open={editing !== null} onClose={() => setEditing(null)} title={editing === "new" ? "Add a control page" : "Edit control page"}>
        {editing !== null && <ControlForm page={editing === "new" ? null : editing} onDone={() => setEditing(null)} />}
      </Dialog>
    </Card>
  );
}

function ControlForm({ page, onDone }: { page: ControlPage | null; onDone: () => void }) {
  const qc = useQueryClient();
  const [url, setUrl] = useState(page?.url ?? "");
  const [hold, setHold] = useState(page?.hold_until ?? "");
  const [note, setNote] = useState(page?.note ?? "");
  const save = useMutation({
    mutationFn: (input: ControlInput) => api.controlPages.upsert(input),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: keys.controlPages });
      onDone();
    },
  });
  const valid = /^https?:\/\/\S+$/.test(url.trim()) && /^\d{4}-\d{2}-\d{2}$/.test(hold);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (valid) save.mutate({ url: url.trim(), hold_until: hold, note: note.trim() || undefined });
  };
  return (
    <DialogBody>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Page URL" hint={page ? "To hold another page, add it as a new control page." : "The full URL, as Search Console reports it. Adding a URL that is already a control page updates it."}>
          <input
            className={inputClass}
            type="url"
            inputMode="url"
            autoCapitalize="none"
            spellCheck={false}
            required
            disabled={!!page}
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://www.example.com/pricing"
            data-autofocus
          />
        </Field>
        <Field label="Hold until" hint="The last day it counts. Leave the page unchanged until then.">
          <input className={inputClass} type="date" required value={hold} onChange={(e) => setHold(e.target.value)} />
        </Field>
        <Field label="Note" hint="Optional, such as why this page.">
          <input className={inputClass} maxLength={500} value={note} onChange={(e) => setNote(e.target.value)} />
        </Field>
        <ErrorNote error={save.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={onDone}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={!valid || save.isPending}>
            {page ? "Save" : "Add control page"}
          </Button>
        </div>
      </form>
    </DialogBody>
  );
}
