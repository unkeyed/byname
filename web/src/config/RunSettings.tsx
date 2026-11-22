import { useState, type FormEvent, type ReactNode } from "react";
import type { Costs, Settings, SettingsView } from "../api";
import { useCan, useSettings } from "../queries";
import { cx, ErrorNote, inputClass, labelClass, Pending } from "../ui";
import { usd } from "../lines";
import { about, monthOfChecks, useCosts } from "./costs";
import { defaultFooter, Disclosure, NumberField, ReadOnlyNote, SubmitButton, useSaveSettings, type Footer } from "./forms";
import { formatNumber, parseNumber, type NumberKey } from "./rules";

const scheduleKeys = ["check_every_days"] as const satisfies NumberKey[];
const capKeys = ["spend_cap_run_usd", "spend_cap_month_usd"] as const satisfies NumberKey[];
const tuningKeys = ["concurrency", "retries", "timeout_s"] as const satisfies NumberKey[];

const hints: Partial<Record<NumberKey, string>> = {
  check_every_days: "1 to 30. Every question is asked in every AI app this often.",
  spend_cap_run_usd: "A check stops asking once it has spent this much.",
  spend_cap_month_usd: "Covers AI checks and social searches together. Empty means no monthly cap.",
  concurrency: "1 to 32. Default 8.",
  retries: "0 to 3. Default 1.",
  timeout_s: "30 to 600. Default 180.",
};

/**
 * How often checks run and what they may spend. Onboarding step 6 shows
 * both, with the tuning collapsed; Settings splits Schedule and Limits.
 */
export function RunSettingsForm({
  schedule,
  limits,
  tuningCollapsed,
  extra,
  footer = defaultFooter,
  onSaved,
  submitLabel,
}: {
  schedule?: boolean;
  limits?: boolean;
  tuningCollapsed?: boolean;
  extra?: ReactNode;
  footer?: Footer;
  onSaved?: (v: SettingsView) => unknown;
  submitLabel?: string;
}) {
  const settings = useSettings();
  const costs = useCosts();
  if (settings.isPending) return <Pending />;
  if (settings.isError) return <ErrorNote error={settings.error} />;
  return (
    <Body
      initial={settings.data}
      costs={costs.data}
      schedule={schedule}
      limits={limits}
      tuningCollapsed={tuningCollapsed}
      extra={extra}
      footer={footer}
      onSaved={onSaved}
      submitLabel={submitLabel}
    />
  );
}

function Body({
  initial,
  costs,
  schedule,
  limits,
  tuningCollapsed,
  extra,
  footer,
  onSaved,
  submitLabel,
}: {
  initial: SettingsView;
  costs?: Costs;
  schedule?: boolean;
  limits?: boolean;
  tuningCollapsed?: boolean;
  extra?: ReactNode;
  footer: Footer;
  onSaved?: (v: SettingsView) => unknown;
  submitLabel?: string;
}) {
  const canEdit = useCan("config:write");
  const keysShown = [...(schedule ? scheduleKeys : []), ...(limits ? [...capKeys, ...tuningKeys] : [])];
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(keysShown.map((k) => [k, formatNumber(initial[k])])));
  const [checkTime, setCheckTime] = useState(initial.check_time || "06:00");
  const [tried, setTried] = useState(false);
  const save = useSaveSettings(onSaved);

  const parsed = Object.fromEntries(keysShown.map((k) => [k, parseNumber(k, values[k] ?? "")]));
  const errorOf = (k: NumberKey) => {
    const p = parsed[k];
    return tried && p && "error" in p ? p.error : undefined;
  };
  const timeError = schedule && !/^([01]\d|2[0-3]):[0-5]\d$/.test(checkTime) ? "Give a time as HH:MM, such as 06:00." : undefined;
  const tuningError = tuningKeys.some((k) => errorOf(k));

  // The estimate beside each cap follows the schedule as it is typed.
  const typedDays = parsed.check_every_days;
  const days = typedDays && "value" in typedDays && typedDays.value ? typedDays.value : initial.check_every_days;
  const perCheck = costs?.checks.check.known_usd ?? 0;
  const checksMonth = monthOfChecks(perCheck, days);
  const socialMonth = costs?.social.month_usd ?? 0;
  const capEmpty = !(values.spend_cap_month_usd ?? "").trim();
  const estimate: Partial<Record<NumberKey, string>> = costs
    ? {
        check_every_days: `${hints.check_every_days} At every ${days === 1 ? "day" : `${days} days`}, checks cost ${about(checksMonth)} a month.`,
        spend_cap_run_usd: `One check is estimated at ${about(perCheck)}. ${hints.spend_cap_run_usd}`,
        spend_cap_month_usd: `Estimated at ${about(checksMonth + socialMonth)} a month${socialMonth > 0 ? ", social searches included" : ""}. ${
          capEmpty && !initial.onboarded_at ? `Leave it empty and finishing setup sets ${usd(costs.default_month_cap_usd)}, which you can change.` : hints.spend_cap_month_usd
        }`,
      }
    : {};

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (!canEdit || timeError || Object.values(parsed).some((p) => "error" in p)) return;
    const patch: Partial<Settings> = Object.fromEntries(Object.entries(parsed).map(([k, p]) => [k, "value" in p ? p.value : null]));
    if (schedule) patch.check_time = checkTime;
    save.mutate(patch);
  };
  const field = (k: NumberKey, placeholder?: string) => (
    <NumberField
      key={k}
      name={k}
      value={values[k] ?? ""}
      onChange={(v) => {
        setValues({ ...values, [k]: v });
        if (save.isSuccess) save.reset();
      }}
      error={errorOf(k)}
      hint={estimate[k] ?? hints[k]}
      placeholder={placeholder}
    />
  );
  const tuning = <div className="grid gap-4 sm:grid-cols-3">{tuningKeys.map((k) => field(k))}</div>;

  return (
    <form onSubmit={submit} noValidate className="space-y-6">
      {!canEdit && <ReadOnlyNote />}
      <fieldset disabled={!canEdit} className="space-y-6">
        {schedule && (
          <div className="space-y-3">
            {field("check_every_days")}
            <p className="text-sm text-muted">
              Checks run at <span className="font-medium text-fg tabular-nums">{checkTime}</span>, {initial.time_zone.replaceAll("_", " ")} time, the time zone you picked for your
              brand.
            </p>
            <Disclosure summary="Change the time of day">
              <label className="block w-36">
                <span className={labelClass}>Time of day</span>
                <input type="time" className={cx(inputClass, "tabular-nums")} value={checkTime} onChange={(e) => setCheckTime(e.target.value)} aria-invalid={!!timeError} />
              </label>
              {timeError && <p className="text-xs text-danger">{timeError}</p>}
            </Disclosure>
          </div>
        )}
        {limits && (
          <div className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              {field("spend_cap_run_usd")}
              {field("spend_cap_month_usd", "No cap")}
            </div>
            {tuningCollapsed ? (
              <Disclosure summary="Advanced: concurrency, retries and timeout" open={tuningError || undefined}>
                {tuning}
              </Disclosure>
            ) : (
              tuning
            )}
          </div>
        )}
      </fieldset>
      {extra}
      {canEdit && footer(<SubmitButton pending={save.isPending} saved={save.isSuccess} label={submitLabel} />)}
      <ErrorNote error={save.error} />
    </form>
  );
}
