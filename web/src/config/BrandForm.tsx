import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, type MatchMode, type Settings, type SettingsView } from "../api";
import { keys, useCan, useSettings } from "../queries";
import { Button, cx, ErrorNote, Field, Icon, inputClass, labelClass, Pending, selectClass } from "../ui";
import { defaultFooter, FieldNote, NoteSlot, NumberField, ReadOnlyNote, SubmitButton, useSaveSettings, type Footer } from "./forms";
import { browserTimeZone, commonWordAliases, formatNumber, languages, markets, parseHost, parseHosts, parseNumber, splitList, timeZones } from "./rules";

const matchModes: { mode: MatchMode; title: string; detail: string }[] = [
  {
    mode: "whole_word",
    title: "Whole word, case-sensitive",
    detail: "Counts “Northwind” and your aliases as whole words, written as you wrote them. “northwinds” or “NORTHWIND” do not count.",
  },
  {
    mode: "self_anywhere",
    title: "Also anywhere, ignoring case",
    detail: "Also counts your brand name inside other words and in any case, such as “northwind-cli”. Use it when answers write your name in many ways. It applies to your own brand only, never to competitors.",
  },
];

/** An edit to the branded terms: back to following the name and aliases, or terms typed. null sends nothing. */
type TermsEdit = null | { follow: true } | { text: string };

/**
 * Your brand, domains, matching, market and time zone: onboarding step 1
 * and Settings > General, which adds raw response retention.
 */
export function BrandForm({ retention, footer = defaultFooter, onSaved, submitLabel }: { retention?: boolean; footer?: Footer; onSaved?: () => unknown; submitLabel?: string }) {
  const settings = useSettings();
  if (settings.isPending) return <Pending />;
  if (settings.isError) return <ErrorNote error={settings.error} />;
  return <BrandFormBody initial={settings.data} retention={retention} footer={footer} onSaved={onSaved} submitLabel={submitLabel} />;
}

function BrandFormBody({ initial, retention, footer, onSaved, submitLabel }: { initial: SettingsView; retention?: boolean; footer: Footer; onSaved?: () => unknown; submitLabel?: string }) {
  const canEdit = useCan("config:write");
  const [name, setName] = useState(initial.brand.name);
  const [primary, setPrimary] = useState(initial.brand.domains[0] ?? "");
  const [aliases, setAliases] = useState(initial.brand.aliases.join(", "));
  const [matchMode, setMatchMode] = useState<MatchMode>(initial.match_mode || "whole_word");
  const [others, setOthers] = useState(initial.brand.domains.slice(1).join(", "));
  const [market, setMarket] = useState(initial.market || "US");
  const [language, setLanguage] = useState(initial.language || "en");
  // A brand that was never saved still carries the server's UTC default; the browser knows better.
  const [timeZone, setTimeZone] = useState(initial.brand.name ? initial.time_zone : browserTimeZone());
  const [keepForever, setKeepForever] = useState(initial.raw_retention_days == null);
  const [retentionDays, setRetentionDays] = useState(formatNumber(initial.raw_retention_days ?? 90));
  const [termsEdit, setTermsEdit] = useState<TermsEdit>(null);
  const [tried, setTried] = useState(false);
  const qc = useQueryClient();

  const nameError = name.trim().length < 2 || name.trim().length > 80 ? "Brand name must be 2 to 80 characters." : null;
  const primaryParse = primary.trim() ? parseHost(primary) : { error: "Your primary domain is required, such as northwind.com." };
  const othersParse = parseHosts(others);
  const aliasList = splitList(aliases);
  const common = commonWordAliases(aliasList);
  const retentionParse = keepForever ? { value: null } : parseNumber("raw_retention_days", retentionDays);
  const otherHosts = "hosts" in othersParse && "host" in primaryParse ? othersParse.hosts.filter((h) => h !== primaryParse.host) : [];
  const repeated = "hosts" in othersParse && "host" in primaryParse && othersParse.hosts.includes(primaryParse.host);
  const defaultTerms = [...new Set([name.trim().replace(/\s+/g, " "), ...aliasList].filter(Boolean).map((t) => t.toLowerCase()))];
  const following = termsEdit === null ? initial.branded_terms_default : "follow" in termsEdit;
  const shownTerms = termsEdit && "text" in termsEdit ? termsEdit.text : (following ? defaultTerms : initial.branded_terms).join(", ");
  const termList = [...new Set(splitList(shownTerms).map((t) => t.toLowerCase()))];
  const termsError =
    termsEdit === null ? null : termList.length > 50 ? "At most 50 terms." : termList.some((t) => t.length < 2 || t.length > 80) ? "Each term must be 2 to 80 characters." : null;
  const invalid = !!nameError || "error" in primaryParse || "error" in othersParse || "error" in retentionParse || !!termsError;
  const save = useSaveSettings(async () => {
    if (termsEdit !== null) {
      const backToDefault = following || termList.length === 0 || termList.join() === defaultTerms.join();
      qc.setQueryData(keys.settings, await api.setBrandedTerms(backToDefault ? [] : termList));
      void qc.invalidateQueries({ queryKey: keys.search });
    }
    return onSaved?.();
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (invalid || !canEdit || !("host" in primaryParse)) return;
    const patch: Partial<Settings> = {
      brand: { name: name.trim().replace(/\s+/g, " "), aliases: aliasList, domains: [primaryParse.host, ...otherHosts] },
      match_mode: matchMode,
      market,
      language,
      time_zone: timeZone,
    };
    if (retention && "value" in retentionParse) patch.raw_retention_days = retentionParse.value;
    save.mutate(patch);
  };
  const changed = () => save.isSuccess && save.reset();

  return (
    <form onSubmit={submit} onChange={changed} noValidate className="space-y-6">
      {!canEdit && <ReadOnlyNote />}
      <fieldset disabled={!canEdit} className="space-y-5">
        <div className="grid gap-5 sm:grid-cols-2">
          <div>
            <Field label="Brand name">
              <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} placeholder="Northwind" autoComplete="organization" aria-invalid={tried && !!nameError} />
            </Field>
            <NoteSlot>{tried && nameError && <FieldNote tone="error">{nameError}</FieldNote>}</NoteSlot>
          </div>
          <div>
            <Field label="Primary domain">
              <input
                className={inputClass}
                value={primary}
                onChange={(e) => setPrimary(e.target.value)}
                onBlur={() => "host" in primaryParse && setPrimary(primaryParse.host)}
                placeholder="northwind.com"
                inputMode="url"
                autoCapitalize="none"
                spellCheck={false}
                aria-invalid={tried && "error" in primaryParse}
              />
            </Field>
            <NoteSlot>
              {"error" in primaryParse ? (
                (tried || primary.trim()) && <FieldNote tone="error">{primaryParse.error}</FieldNote>
              ) : primaryParse.trimmed ? (
                <FieldNote tone="info">Kept the host only: {primaryParse.host}. Links to it and its subdomains count as citations.</FieldNote>
              ) : (
                <FieldNote tone="info">Links to it and its subdomains count as citations.</FieldNote>
              )}
            </NoteSlot>
          </div>
        </div>

        <div>
          <Field label="Aliases" hint={!common.length && "Comma separated. Each counts as your brand, such as “Northwind Cloud” or “NW Deploy”."}>
            <input className={inputClass} value={aliases} onChange={(e) => setAliases(e.target.value)} placeholder="Northwind Cloud, NW Deploy" />
          </Field>
          {common.length > 0 && (
            <FieldNote tone="warn">
              {common.map((c) => `“${c}”`).join(", ")} {common.length === 1 ? "is an everyday word" : "are everyday words"}, so answers that are not about you would count as
              mentions. Prefer a longer alias, such as “{common[0]} by {name.trim() || "Northwind"}”.
            </FieldNote>
          )}
        </div>

        <div>
          <Field
            label="Branded search terms"
            hint={
              !termsError &&
              (following
                ? "Comma separated. A Search Console query containing one counts as brand. These follow your name and aliases until you change them."
                : "Comma separated, saved lowercased. A Search Console query containing one counts as brand.")
            }
          >
            <input className={inputClass} value={shownTerms} onChange={(e) => setTermsEdit({ text: e.target.value })} placeholder={defaultTerms.join(", ") || "northwind"} autoCapitalize="none" spellCheck={false} />
          </Field>
          {termsError && <FieldNote tone="error">{termsError}</FieldNote>}
          {!following && (
            <Button size="sm" variant="ghost" className="mt-1 -ml-2" onClick={() => setTermsEdit({ follow: true })}>
              <Icon name="reset" className="size-3.5" />
              Use name and aliases
            </Button>
          )}
        </div>

        <fieldset>
          <legend className={labelClass}>Match mode</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {matchModes.map((m) => (
              <label
                key={m.mode}
                className={cx(
                  "flex cursor-pointer gap-2.5 rounded-surface border p-3 transition-colors has-disabled:cursor-default",
                  matchMode === m.mode ? "border-fg bg-subtle" : "border-line-strong hover:border-line-hover",
                )}
              >
                <input type="radio" name="match_mode" className="mt-0.5" checked={matchMode === m.mode} onChange={() => setMatchMode(m.mode)} />
                <span className="min-w-0">
                  <span className="block text-sm font-medium">{m.title}</span>
                  <span className="mt-0.5 block text-xs text-muted">{m.detail}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>

        <div>
          <Field label="Other domains" hint={!("error" in othersParse) && !repeated && "Comma separated. Docs or regional sites, such as northwind.dev or northwind.co.uk."}>
            <input
              className={inputClass}
              value={others}
              onChange={(e) => setOthers(e.target.value)}
              onBlur={() => "hosts" in othersParse && setOthers(otherHosts.join(", "))}
              placeholder="northwind.dev"
              autoCapitalize="none"
              spellCheck={false}
            />
          </Field>
          {"error" in othersParse ? (
            <FieldNote tone="error">{othersParse.error}</FieldNote>
          ) : repeated ? (
            <FieldNote tone="info">Your primary domain is already counted, so it is left out here.</FieldNote>
          ) : (
            othersParse.trimmed.length > 0 && <FieldNote tone="info">Kept the host only: {othersParse.trimmed.join(", ")}.</FieldNote>
          )}
        </div>

        <div className="grid gap-5 sm:grid-cols-3">
          <Field label="Market" hint="Where engines that take a location ask from.">
            <select className={cx(selectClass, "w-full")} value={market} onChange={(e) => setMarket(e.target.value)}>
              {markets(market).map((m) => (
                <option key={m.code} value={m.code}>
                  {m.label}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Language" hint="For engines that take one.">
            <select className={cx(selectClass, "w-full")} value={language} onChange={(e) => setLanguage(e.target.value)}>
              {languages(language).map((l) => (
                <option key={l.code} value={l.code}>
                  {l.label}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Time zone" hint="Checks run on this clock.">
            <select className={cx(selectClass, "w-full")} value={timeZone} onChange={(e) => setTimeZone(e.target.value)}>
              {timeZones(timeZone).map((z) => (
                <option key={z} value={z}>
                  {z.replaceAll("_", " ")}
                </option>
              ))}
            </select>
          </Field>
        </div>

        {retention && (
          <fieldset>
            <legend className={labelClass}>Raw response retention</legend>
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:gap-6">
              <label className="flex min-h-11 items-center gap-2 text-sm md:min-h-0">
                <input type="radio" name="retention" checked={keepForever} onChange={() => setKeepForever(true)} />
                Keep forever
              </label>
              <div className="flex items-start gap-2">
                <label className="-m-4 flex p-4 md:m-0 md:p-0">
                  <input type="radio" name="retention" aria-label="Keep for a number of days" className="md:mt-2" checked={!keepForever} onChange={() => setKeepForever(false)} />
                </label>
                <NumberField
                  name="raw_retention_days"
                  value={retentionDays}
                  onChange={(v) => {
                    setRetentionDays(v);
                    setKeepForever(false);
                  }}
                  error={!keepForever && "error" in retentionParse ? retentionParse.error : undefined}
                  hint="Scores and citations stay. Only the provider's raw response is deleted."
                />
              </div>
            </div>
          </fieldset>
        )}
      </fieldset>

      {canEdit && footer(<SubmitButton pending={save.isPending} saved={save.isSuccess} label={submitLabel} />)}
      <ErrorNote error={save.error} />
    </form>
  );
}
