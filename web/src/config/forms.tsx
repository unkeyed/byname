import type { ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Settings, type SettingsView } from "../api";
import { keys } from "../queries";
import { Button, cx, Icon, inputClass, labelClass } from "../ui";
import { editable, numberFields, type NumberKey } from "./rules";

/**
 * Saves some settings fields. PUT /v1/settings takes the whole object, so it
 * reads the current settings first and changes only the fields given.
 */
export function useSaveSettings(onSaved?: (v: SettingsView) => unknown) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (patch: Partial<Settings>) => {
      const current = await qc.fetchQuery({ queryKey: keys.settings, queryFn: api.settings.get, staleTime: 0 });
      return api.settings.put({ ...editable(current), ...patch });
    },
    onSuccess: (v) => {
      qc.setQueryData(keys.settings, v);
      void qc.invalidateQueries({ queryKey: keys.brands });
      void qc.invalidateQueries({ queryKey: keys.estimate });
      return onSaved?.(v);
    },
  });
}

/** Where a form's submit button goes; onboarding wraps it in Back and Skip. */
export type Footer = (submit: ReactNode) => ReactNode;

export const defaultFooter: Footer = (submit) => <div className="flex flex-wrap items-center justify-end gap-2">{submit}</div>;

export function SubmitButton({ pending, saved, label = "Save", disabled }: { pending: boolean; saved?: boolean; label?: string; disabled?: boolean }) {
  return (
    <>
      {saved && (
        <span className="flex items-center gap-1 text-xs text-success" aria-live="polite">
          <Icon name="check" className="size-3.5" />
          Saved
        </span>
      )}
      <Button type="submit" variant="primary" disabled={pending || disabled}>
        {pending ? "Saving…" : label}
      </Button>
    </>
  );
}

/** A number setting with its unit and bounds, from numberFields. */
export function NumberField({
  name,
  value,
  onChange,
  error,
  disabled,
  label,
  hint,
  placeholder,
}: {
  name: NumberKey;
  value: string;
  onChange: (v: string) => void;
  error?: string;
  disabled?: boolean;
  label?: string;
  hint?: ReactNode;
  placeholder?: string;
}) {
  const f: { label: string; unit: string; min?: number; max?: number; int?: boolean } = numberFields[name];
  const id = `field-${name}`;
  return (
    <div>
      <label htmlFor={id} className={labelClass}>
        {label ?? f.label}
      </label>
      <div className="grid grid-cols-[7rem_auto] items-center justify-start gap-2">
        <input
          id={id}
          type="number"
          inputMode={f.int ? "numeric" : "decimal"}
          step={f.int ? 1 : "any"}
          min={f.min}
          max={f.max}
          value={value}
          placeholder={placeholder}
          disabled={disabled}
          aria-invalid={error ? true : undefined}
          aria-describedby={error || hint ? `${id}-note` : undefined}
          onChange={(e) => onChange(e.target.value)}
          className={cx(inputClass, "tabular-nums", error && "border-danger")}
        />
        <span className="text-sm text-muted">{f.unit}</span>
      </div>
      {(error || hint) && (
        <p id={`${id}-note`} className={cx("mt-1 text-xs", error ? "text-danger" : "text-faint")}>
          {error ?? hint}
        </p>
      )}
    </div>
  );
}

/** Beside a field: a check that failed in the browser, or a warning that does not block saving. */
export function FieldNote({ tone, children }: { tone: "error" | "warn" | "info"; children: ReactNode }) {
  return (
    <p className={cx("mt-1 flex items-start gap-1 text-xs", tone === "error" ? "text-danger" : tone === "warn" ? "text-warn" : "text-muted")}>
      {tone !== "info" && <Icon name="alert" className="mt-px size-3" />}
      <span className="min-w-0 [overflow-wrap:anywhere]">{children}</span>
    </p>
  );
}

/** Shown to people who can read setup but not change it. */
export function ReadOnlyNote({ children = "You can see these settings. An admin or editor can change them." }: { children?: ReactNode }) {
  return <p className="mb-4 rounded-control bg-subtle px-3 py-2 text-xs text-muted">{children}</p>;
}

/** A collapsed section, for settings that have good defaults. */
export function Disclosure({ summary, children, open, onToggle }: { summary: ReactNode; children: ReactNode; open?: boolean; onToggle?: (open: boolean) => void }) {
  return (
    <details open={open} onToggle={onToggle && ((e) => onToggle(e.currentTarget.open))} className="group rounded-surface border border-line">
      <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-2 text-sm font-medium select-none [&::-webkit-details-marker]:hidden">
        <Icon name="right" className="size-3.5 text-muted transition-transform group-open:rotate-90" />
        {summary}
      </summary>
      <div className="space-y-4 border-t border-line px-3 py-3">{children}</div>
    </details>
  );
}
