import { useState, type FormEvent } from "react";
import { Link } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type OtherBrandsSettings as Settings } from "../api";
import { money } from "../money";
import { keys, useCan, useCosts, useOtherBrandsSettings } from "../queries";
import { Card, CardHeader, cx, ErrorNote, inputClass, labelClass, Pending } from "../ui";
import { NoteSlot, ReadOnlyNote, SubmitButton } from "./forms";

/**
 * Finding other brands: reading each new answer for the brands it names,
 * and looking up logos, each with what it costs where it is switched on.
 */
export function OtherBrandsSettings() {
  const settings = useOtherBrandsSettings();
  if (settings.isPending) return <Pending />;
  if (settings.isError) return <ErrorNote error={settings.error} />;
  return <Form s={settings.data} />;
}

function Form({ s }: { s: Settings }) {
  const canEdit = useCan("config:write");
  const costs = useCosts();
  const qc = useQueryClient();
  const [find, setFind] = useState(s.find.on);
  const [logos, setLogos] = useState(s.logos.on);
  const [limit, setLimit] = useState(String(s.logos.month_limit));
  const [saved, setSaved] = useState(false);
  const save = useMutation({
    mutationFn: api.otherBrands.putSettings,
    onSuccess: () => {
      setSaved(true);
      void qc.invalidateQueries({ queryKey: keys.otherBrandsSettings });
      void qc.invalidateQueries({ queryKey: keys.costs });
      void qc.invalidateQueries({ queryKey: keys.estimate });
    },
  });
  const n = Number(limit);
  const bad = !Number.isInteger(n) || n < 0 || n > 1000;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setSaved(false);
    if (!bad) save.mutate({ find, logos, logo_month_limit: n });
  };
  const calls = costs.data?.checks.check.calls ?? 0;
  return (
    <Card>
      <span id="other-brands" className="block scroll-mt-4" />
      <CardHeader
        title="Other brands"
        description="Byname can list every brand AI answers name that you don't track, under AI answers > Competitors, and show each brand's logo. Both go through Treg and are paid."
      />
      <form onSubmit={submit} className="space-y-5 border-t border-line px-4 py-4 text-sm">
        {!canEdit && <ReadOnlyNote />}
        {!s.has_token && (
          <p className="rounded-control bg-warn-soft px-3 py-2 text-xs text-warn">
            Both need a Treg token, the service that runs each question and bills you per answer. An admin adds it under <Link to="/settings/treg" className="font-medium underline">Settings &gt; Treg</Link>.
          </p>
        )}
        <label className="flex items-start gap-3">
          <input type="checkbox" className="mt-1 size-3.5 accent-[var(--fg)]" checked={find} disabled={!canEdit} onChange={(e) => setFind(e.target.checked)} />
          <span>
            <span className="font-medium">Find other brands in answers</span>
            <span className="mt-0.5 block text-muted">
              A small model reads each new answer and lists the brands it names{calls > 0 && `, about ${money(s.find.cost_per_answer_usd * calls)} for a check of ${calls} answers`}. If a read fails, the check still finishes and the answer is read again later.
            </span>
          </span>
        </label>
        <label className="flex items-start gap-3">
          <input type="checkbox" className="mt-1 size-3.5 accent-[var(--fg)]" checked={logos} disabled={!canEdit} onChange={(e) => setLogos(e.target.checked)} />
          <span>
            <span className="font-medium">Show logos</span>
            <span className="mt-0.5 block text-muted">
              Looks up each brand once on Context.dev: about {money(s.logos.cost_per_brand_usd)} for each new brand, nothing for one it doesn't know. Logos are kept in Byname, so your browser never
              loads them from anyone else. {s.logos.logos > 0 && `${s.logos.logos} ${s.logos.logos === 1 ? "brand has" : "brands have"} a logo.`}
            </span>
          </span>
        </label>
        <div className={cx(!logos && "opacity-60")}>
          <label htmlFor="logo-limit" className={labelClass}>
            New brands looked up a month, at most
          </label>
          <div className="flex items-center gap-2">
            <div className="w-28 shrink-0">
              <input id="logo-limit" type="number" inputMode="numeric" min={0} max={1000} step={1} value={limit} disabled={!canEdit || !logos} onChange={(e) => setLimit(e.target.value)} className={cx(inputClass, "tabular-nums", bad && "border-danger")} aria-invalid={bad || undefined} />
            </div>
            <span className="text-muted">at most {money((bad ? 0 : n) * s.logos.cost_per_brand_usd)} a month</span>
          </div>
          <NoteSlot>
            <p className={cx("mt-1 text-xs", bad ? "text-danger" : "text-faint")}>
              {bad ? "Enter a whole number from 0 to 1000." : `${s.logos.this_month} looked up so far this month. Your brand and your competitors come first, then the brands answers name most.`}
            </p>
          </NoteSlot>
        </div>
        <ErrorNote error={save.error} />
        {canEdit && (
          <div className="flex flex-wrap items-center justify-end gap-2">
            <SubmitButton pending={save.isPending} saved={saved} disabled={bad} />
          </div>
        )}
      </form>
    </Card>
  );
}
