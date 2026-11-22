import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type BrandPoint, type Day, type GscStatus } from "../../api";
import { LineChart, LineKey, niceTicks, type ChartLine } from "../../charts";
import { keys, useSettings } from "../../queries";
import { Badge, Button, Card, CardHeader, cx, Dialog, DialogBody, ErrorNote, Icon, inputClass, Pending, ScrollX } from "../../ui";
import { compact, count, day, figureInfo, longDay, useSearchBrand } from "./model";

/** The three kinds of click a window splits into, in legend order. Your brand is drawn in ink, as everywhere. */
const kinds: { key: keyof Omit<BrandPoint, "date" | "clicks">; label: string; color: string; detail: string }[] = [
  { key: "brand_clicks", label: "With your name", color: "var(--you)", detail: "Clicks from searches that include your name or another branded term." },
  { key: "non_brand_clicks", label: figureInfo.non_brand.label, color: "var(--series-1)", detail: figureInfo.non_brand.hint },
  { key: "anonymized_clicks", label: "Hidden by Google", color: "var(--faint)", detail: "Clicks from searches Google keeps private. Never counted as searches without your name." },
];

export function BrandCard({ status, from, canEdit }: { status: GscStatus; from: Day; canEdit: boolean }) {
  const brand = useSearchBrand(status, from);
  const [editing, setEditing] = useState(false);

  let body;
  if (brand.isPending) body = <Pending />;
  else if (brand.isError) body = <div className="px-4 pb-4"><ErrorNote error={brand.error} /></div>;
  else if (brand.data.points.length === 0) body = <p className="px-4 pb-6 text-sm text-muted">Each point adds up 28 days, so this shows once Byname has 28 days from Google.</p>;
  else {
    const pts = brand.data.points;
    const xs = pts.map((p) => ({ key: p.date, label: day(p.date), title: `28 days to ${longDay(p.date)}` }));
    const lines: ChartLine[] = kinds.map((k) => ({ key: k.key, label: k.label, color: k.color, approx: false, values: pts.map((p) => p[k.key]) }));
    const latest = pts[pts.length - 1];
    body = (
      <div className={cx("grid grid-cols-[minmax(0,1fr)] gap-4 px-4 pb-4 lg:grid-cols-[minmax(0,1fr)_14rem]", brand.isPlaceholderData && "opacity-60")}>
        <LineChart xs={xs} lines={lines} ticks={niceTicks(Math.max(...pts.flatMap((p) => kinds.map((k) => p[k.key]))))} format={compact} height={220} label="Clicks from searches with your name, without it and hidden by Google, over each 28 days" />
        <div className="space-y-3 self-start">
          <ul aria-label={`The 28 days to ${day(latest.date)}`} className="space-y-2 text-xs">
            {kinds.map((k) => (
              <li key={k.key} className="flex items-start gap-2">
                <LineKey color={k.color} className="mt-1.5" />
                <span className="min-w-0 flex-1">
                  <span className="block font-medium">{k.label}</span>
                  <span className="block text-faint">{k.detail}</span>
                </span>
                <span className="text-sm font-semibold tabular-nums">{count(latest[k.key])}</span>
              </li>
            ))}
          </ul>
          <p className="text-xs text-faint">Numbers for the 28 days to {day(latest.date)}.</p>
        </div>
        <div className="space-y-1.5 text-xs lg:col-span-2">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-muted">A search includes your name when it contains</span>
            {brand.data.terms.map((t) => (
              <Badge key={t} tone="outline">
                {t}
              </Badge>
            ))}
          </div>
          <details>
            <summary className="cursor-pointer py-3.5 text-muted hover:text-fg md:py-0">Show as a table</summary>
            <div className="mt-2 max-h-72 overflow-y-auto">
              <ScrollX label="brand table">
                <table className="w-full min-w-max text-left tabular-nums">
                  <thead className="sticky top-0 bg-surface text-muted">
                    <tr>
                      <th className="py-1 pr-4 font-medium">28 days to</th>
                      <th className="py-1 pr-4 text-right font-medium">Clicks</th>
                      {kinds.map((k) => (
                        <th key={k.key} className="py-1 pr-4 text-right font-medium">
                          {k.label}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line">
                    {[...pts].reverse().map((p) => (
                      <tr key={p.date}>
                        <td className="py-1 pr-4">{longDay(p.date)}</td>
                        <td className="py-1 pr-4 text-right">{count(p.clicks)}</td>
                        {kinds.map((k) => (
                          <td key={k.key} className="py-1 pr-4 text-right">
                            {count(p[k.key])}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </ScrollX>
            </div>
          </details>
        </div>
      </div>
    );
  }

  return (
    <Card>
      <CardHeader title="Searches with and without your name" description="Clicks in the 28 days up to each day, split by whether the search included your name. Searches without your name come from people who didn't know you yet. Only days Google has finished counting.">
        {canEdit && (
          <Button size="sm" onClick={() => setEditing(true)}>
            <Icon name="tag" className="size-3.5" />
            Branded terms
          </Button>
        )}
      </CardHeader>
      {body}
      <Dialog open={editing} onClose={() => setEditing(false)} title="Branded terms">
        <TermsForm onDone={() => setEditing(false)} />
      </Dialog>
    </Card>
  );
}

function TermsForm({ onDone }: { onDone: () => void }) {
  const qc = useQueryClient();
  const settings = useSettings();
  const [terms, setTerms] = useState<string[] | null>(null);
  const [draft, setDraft] = useState("");
  const save = useMutation({
    mutationFn: api.setBrandedTerms,
    onSuccess: async (s) => {
      qc.setQueryData(keys.settings, s);
      await qc.invalidateQueries({ queryKey: keys.search });
      onDone();
    },
  });
  if (settings.isPending) return <Pending />;
  if (settings.isError) return <DialogBody><ErrorNote error={settings.error} /></DialogBody>;
  const list = terms ?? settings.data.branded_terms;
  const pending = draft.trim().toLowerCase();
  const add = (e: FormEvent) => {
    e.preventDefault();
    if (pending && !list.includes(pending)) setTerms([...list, pending]);
    setDraft("");
  };
  return (
    <DialogBody>
      <div className="space-y-4">
        <p className="text-sm text-muted">
          A Google search that contains any of these counts as including your name. Capitals don&apos;t matter. {settings.data.branded_terms_default && terms === null ? "These are the default: your brand's name and aliases." : "The default is your brand's name and aliases."}
        </p>
        <div className="flex min-h-8 flex-wrap gap-1.5" aria-label="Terms">
          {list.length === 0 ? (
            <span className="text-sm text-faint">No terms. Saving goes back to the default.</span>
          ) : (
            list.map((t) => (
              <Badge key={t} tone="outline" onRemove={() => setTerms(list.filter((x) => x !== t))} removeLabel={`Remove ${t}`}>
                {t}
              </Badge>
            ))
          )}
        </div>
        <form onSubmit={add} className="flex gap-2">
          <input aria-label="Add a term" placeholder="Add a term, such as a product name" value={draft} onChange={(e) => setDraft(e.target.value)} className={inputClass} />
          <Button type="submit" disabled={!draft.trim()}>
            Add
          </Button>
        </form>
        <div className="flex flex-wrap justify-end gap-2">
          {!settings.data.branded_terms_default && (
            <Button variant="ghost" onClick={() => save.mutate([])} disabled={save.isPending} className="mr-auto">
              <Icon name="reset" className="size-3.5" />
              Use the default
            </Button>
          )}
          <Button onClick={onDone}>Cancel</Button>
          <Button variant="primary" onClick={() => save.mutate(pending && !list.includes(pending) ? [...list, pending] : list)} disabled={save.isPending || (terms === null && !pending)}>
            Save terms
          </Button>
        </div>
        <ErrorNote error={save.error} />
      </div>
    </DialogBody>
  );
}
