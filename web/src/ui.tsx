import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type ButtonHTMLAttributes, type ReactNode } from "react";
import { Link } from "react-router";
import { ApiError } from "./api";
import { absoluteTime, initials, relativeTime } from "./format";

export const cx = (...parts: (string | false | null | undefined)[]) => parts.filter(Boolean).join(" ");

const paths = {
  chart: <><path d="M3 3v18h18" /><path d="M8 16v-3M12 16V8M16 16v-6" /></>,
  message: <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />,
  search: <><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></>,
  users: <><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" /><circle cx="9" cy="7" r="4" /><path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" /></>,
  building: <><rect x="4" y="2" width="16" height="20" rx="2" /><path d="M9 22v-4h6v4M8 6h.01M12 6h.01M16 6h.01M8 10h.01M12 10h.01M16 10h.01M8 14h.01M12 14h.01M16 14h.01" /></>,
  settings: <path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6" />,
  pin: <><path d="M12 17v5" /><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z" /></>,
  reset: <><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" /><path d="M3 3v5h5" /></>,
  copy: <><rect x="8" y="8" width="14" height="14" rx="2" /><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" /></>,
  check: <path d="M20 6 9 17l-5-5" />,
  x: <path d="M18 6 6 18M6 6l12 12" />,
  plus: <path d="M12 5v14M5 12h14" />,
  up: <path d="m18 15-6-6-6 6" />,
  down: <path d="m6 9 6 6 6-6" />,
  updown: <path d="m7 15 5 5 5-5M7 9l5-5 5 5" />,
  left: <path d="m15 18-6-6 6-6" />,
  right: <path d="m9 18 6-6-6-6" />,
  sun: <><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" /></>,
  moon: <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />,
  monitor: <><rect x="2" y="3" width="20" height="14" rx="2" /><path d="M8 21h8M12 17v4" /></>,
  logout: <><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" /><path d="m16 17 5-5-5-5M21 12H9" /></>,
  merge: <><path d="m8 6 4-4 4 4" /><path d="M12 2v10.3a4 4 0 0 1-1.17 2.87L4 22M20 22l-5-5" /></>,
  refresh: <><path d="M21 12a9 9 0 1 1-3-6.7L21 8" /><path d="M21 3v5h-5" /></>,
  trash: <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />,
  external: <path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />,
  key: <><circle cx="7.5" cy="15.5" r="5.5" /><path d="m21 2-9.6 9.6M15.5 7.5l3 3L22 7l-3-3" /></>,
  bot: <><rect x="3" y="11" width="18" height="10" rx="2" /><circle cx="12" cy="5" r="2" /><path d="M12 7v4M8 16h.01M16 16h.01" /></>,
  plug: <path d="M12 22v-5M9 8V2M15 8V2M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z" />,
  layers: <><path d="m12 2 10 5-10 5L2 7z" /><path d="m2 17 10 5 10-5M2 12l10 5 10-5" /></>,
  sparkles: <path d="M12 3l1.9 5.8L20 10.5l-6.1 1.7L12 18l-1.9-5.8L4 10.5l6.1-1.7z" />,
  alert: <><circle cx="12" cy="12" r="10" /><path d="M12 8v4M12 16h.01" /></>,
  note: <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />,
  mail: <><rect x="2" y="4" width="20" height="16" rx="2" /><path d="m22 7-10 5L2 7" /></>,
  pencil: <path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z" />,
  login: <><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4" /><path d="m10 17 5-5-5-5M15 12H3" /></>,
  menu: <path d="M4 6h16M4 12h16M4 18h16" />,
  keyboard: <><rect x="2" y="5" width="20" height="14" rx="2" /><path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M6 13h.01M18 13h.01M9 15h6" /></>,
  filter: <path d="M3 5h18M7 12h10M10 19h4" />,
  globe: <><circle cx="12" cy="12" r="10" /><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20M2 12h20" /></>,
  link: <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />,
  linkedin: <><path d="M16 8a6 6 0 0 1 6 6v7h-4v-7a2 2 0 0 0-4 0v7h-4v-7a6 6 0 0 1 6-6z" /><rect width="4" height="12" x="2" y="9" /><circle cx="4" cy="4" r="2" /></>,
  xLogo: <path d="M4 4l11.7 16H20L8.3 4zM4 20l6.8-6.8m2.4-2.4L20 4" />,
  github: <><path d="M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5.08-1.25-.27-2.48-1-3.5.28-1.15.28-2.35 0-3.5 0 0-1 0-3 1.5-2.64-.5-5.36-.5-8 0C6 2 5 2 5 2c-.3 1.15-.3 2.35 0 3.5A5.4 5.4 0 0 0 4 9c0 3.5 3 5.5 6 5.5-.39.49-.68 1.05-.85 1.65-.17.6-.22 1.23-.15 1.85v4" /><path d="M9 18c-4.51 2-5-2-7-2" /></>,
  youtube: <><path d="M2.5 17a24 24 0 0 1 0-10 2 2 0 0 1 1.4-1.4 49.6 49.6 0 0 1 16.2 0A2 2 0 0 1 21.5 7a24 24 0 0 1 0 10 2 2 0 0 1-1.4 1.4 49.6 49.6 0 0 1-16.2 0A2 2 0 0 1 2.5 17" /><path d="m10 15 5-3-5-3z" /></>,
  facebook: <path d="M18 2h-3a5 5 0 0 0-5 5v3H7v4h3v8h4v-8h3l1-4h-4V7a1 1 0 0 1 1-1h3z" />,
  briefcase: <><path d="M16 20V4a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v16" /><rect width="20" height="14" x="2" y="6" rx="2" /></>,
  tag: <><path d="M12.6 2.6A2 2 0 0 0 11.2 2H4a2 2 0 0 0-2 2v7.2a2 2 0 0 0 .6 1.4l8.7 8.7a2.4 2.4 0 0 0 3.4 0l6.6-6.6a2.4 2.4 0 0 0 0-3.4z" /><circle cx="7.5" cy="7.5" r=".5" /></>,
};
export type IconName = keyof typeof paths;

export function Icon({ name, className = "size-4" }: { name: IconName; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.75} strokeLinecap="round" strokeLinejoin="round" className={cx("shrink-0", className)} aria-hidden="true">
      {paths[name]}
    </svg>
  );
}

const buttonStyles = {
  primary:
    "border-transparent bg-primary text-primary-fg shadow-button hover:bg-primary-hover disabled:bg-subtle disabled:text-faint disabled:shadow-none",
  secondary: "border-line-strong bg-surface text-fg hover:border-line-hover hover:bg-subtle disabled:text-faint",
  ghost: "border-transparent text-muted hover:bg-subtle hover:text-fg disabled:text-faint",
  danger: "border-line-strong bg-surface text-danger hover:bg-danger-soft disabled:opacity-60",
};

/** Below md every control is at least 44px, the smallest target a finger hits reliably. From md up the desktop sizes apply. */
const buttonSizes = {
  sm: { label: "h-11 min-w-11 px-2 text-xs md:h-7 md:min-w-0", icon: "size-11 md:size-7" },
  md: { label: "h-11 min-w-11 px-3 text-sm md:h-8 md:min-w-0", icon: "size-11 md:size-8" },
};

/** icon makes the button square, for one that shows only an icon. */
type ButtonLook = { variant?: keyof typeof buttonStyles; size?: keyof typeof buttonSizes; icon?: boolean };

export function buttonClass({ variant = "secondary", size = "md", icon }: ButtonLook = {}) {
  return cx(
    "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-control border font-medium whitespace-nowrap transition-colors active:translate-y-px disabled:pointer-events-none",
    icon ? buttonSizes[size].icon : buttonSizes[size].label,
    buttonStyles[variant],
  );
}

export function Button({ variant, size, icon, className, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & ButtonLook) {
  return <button type="button" {...props} className={cx(buttonClass({ variant, size, icon }), className)} />;
}

/** Every label in one grid cell with only the current one visible, so a button never changes width as its label swaps. */
export function SteadyLabel({ labels, current }: { labels: string[]; current: string }) {
  return (
    <span className="grid">
      {labels.map((l) => (
        <span key={l} className={cx("col-start-1 row-start-1", l !== current && "invisible")} aria-hidden={l !== current}>
          {l}
        </span>
      ))}
    </span>
  );
}

export function Logo({ className = "size-6 text-xs" }: { className?: string }) {
  return (
    <span aria-hidden="true" className={cx("grid shrink-0 place-items-center rounded-control bg-primary font-bold text-primary-fg", className)}>
      B
    </span>
  );
}

const fieldBase =
  "h-11 md:h-8 min-w-0 rounded-control border border-line-strong bg-surface px-2.5 text-sm text-fg transition-colors placeholder:text-faint hover:border-line-hover focus-within:border-fg focus-within:ring-3 focus-within:ring-halo focus:outline-none focus-visible:outline-none disabled:opacity-60";
export const inputClass = cx(fieldBase, "w-full");
export const selectClass = cx(fieldBase, "select-chevron pr-7");
export const labelClass = "mb-1.5 block text-xs font-medium text-muted";

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block">
      <span className={labelClass}>{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-faint">{hint}</span>}
    </label>
  );
}

/**
 * A destructive action that asks once, in place: the first click arms it, the
 * second runs it, and Escape or leaving disarms it. Both faces share one grid
 * cell, so arming never changes the button's width.
 */
export function ConfirmButton({
  label,
  confirmLabel,
  onConfirm,
  children,
  className,
  disabled,
}: {
  label: string;
  confirmLabel: string;
  onConfirm: () => void;
  children: ReactNode;
  className?: string;
  disabled?: boolean;
}) {
  const [armed, setArmed] = useState(false);
  return (
    <Button
      size="sm"
      variant={armed ? "danger" : "ghost"}
      aria-label={armed ? confirmLabel : label}
      title={armed ? undefined : label}
      data-armed={armed || undefined}
      disabled={disabled}
      onBlur={() => setArmed(false)}
      onKeyDown={(e) => {
        if (e.key === "Escape" && armed) {
          e.preventDefault();
          e.stopPropagation();
          setArmed(false);
        }
      }}
      onClick={() => (armed ? (setArmed(false), onConfirm()) : setArmed(true))}
      className={className}
    >
      <span className="grid">
        <span className={cx("col-start-1 row-start-1 flex items-center justify-center gap-1.5", armed && "invisible")}>{children}</span>
        <span className={cx("col-start-1 row-start-1", !armed && "invisible")}>{confirmLabel}</span>
      </span>
    </Button>
  );
}

export function useCopy(): [boolean, (text: string) => void] {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const copy = (text: string) => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1500);
    });
  };
  return [copied, copy];
}

export function CopyButton({ text, label = "Copy", className }: { text: string; label?: string; className?: string }) {
  const [copied, copy] = useCopy();
  return (
    <Button size="sm" variant="ghost" onClick={() => copy(text)} className={className} aria-label={copied ? "Copied" : label}>
      <Icon name={copied ? "check" : "copy"} className={cx("size-3.5", copied && "text-success")} />
      <span aria-live="polite">{copied ? "Copied" : label}</span>
    </Button>
  );
}

export function CopyField({ value, label }: { value: string; label?: string }) {
  return (
    <div className="flex min-w-0 items-center gap-1 rounded-control border border-line bg-header pl-2.5">
      <code className="min-w-0 flex-1 truncate py-1.5 font-mono text-xs" title={value}>
        {value}
      </code>
      <CopyButton text={value} label={label} />
    </div>
  );
}

export function CodeBlock({ code, label }: { code: string; label?: string }) {
  const pre = (
    <pre className="min-w-0 flex-1 px-3 py-2.5 font-mono text-xs leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere]">
      <code>{code}</code>
    </pre>
  );
  if (!label) {
    return (
      <div className="flex items-start rounded-surface border border-line bg-header">
        {pre}
        <CopyButton text={code} className="m-1.5" />
      </div>
    );
  }
  return (
    <div className="rounded-surface border border-line bg-header">
      <div className="flex items-center justify-between gap-2 border-b border-line py-1 pr-1.5 pl-3 text-xs font-medium text-muted">
        <span className="min-w-0 truncate">{label}</span>
        <CopyButton text={code} />
      </div>
      {pre}
    </div>
  );
}

function Initials({ label, size, className }: { label: string; size: number; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cx("inline-flex shrink-0 items-center justify-center bg-subtle font-medium tracking-normal text-muted ring-1 ring-line ring-inset select-none", className)}
      style={{ width: size, height: size, fontSize: Math.max(10, Math.round(size * 0.38)) }}
    >
      {initials(label)}
    </span>
  );
}

export function Avatar({ src, label, size = 28 }: { src?: string; label: string; size?: number }) {
  const [failedSrc, setFailedSrc] = useState<string>();
  if (src && src !== failedSrc) {
    return (
      <img
        src={src}
        alt=""
        width={size}
        height={size}
        loading="lazy"
        referrerPolicy="no-referrer"
        onError={() => setFailedSrc(src)}
        className="shrink-0 rounded-full bg-subtle object-cover ring-1 ring-line"
        style={{ width: size, height: size }}
      />
    );
  }
  return <Initials label={label} size={size} className="rounded-full" />;
}

const tones = {
  neutral: "bg-subtle text-muted",
  outline: "border border-line-strong text-muted",
  success: "bg-success-soft text-success",
  warn: "bg-warn-soft text-warn",
  danger: "bg-danger-soft text-danger",
  info: "bg-info-soft text-info",
};
export type Tone = keyof typeof tones;

export function Badge({
  tone = "neutral",
  children,
  title,
  className,
  onRemove,
  removeLabel,
}: {
  tone?: Tone;
  children: ReactNode;
  title?: string;
  className?: string;
  onRemove?: () => void;
  removeLabel?: string;
}) {
  return (
    <span
      title={title}
      className={cx("inline-flex max-w-full items-center gap-1 rounded-control px-1.5 text-xs font-medium whitespace-nowrap", onRemove ? "h-11 pr-0 md:h-5 md:pr-0.5" : "h-5", tones[tone], className)}
    >
      {children}
      {onRemove && (
        <button type="button" aria-label={removeLabel} onClick={onRemove} className="grid size-11 shrink-0 place-items-center rounded-inner transition-colors hover:bg-subtle-strong hover:text-fg md:size-4">
          <Icon name="x" className="size-3" />
        </button>
      )}
    </span>
  );
}

export function RelativeTime({ iso, className }: { iso: string; className?: string }) {
  return (
    <time dateTime={iso} title={absoluteTime(iso)} className={className}>
      {relativeTime(iso)}
    </time>
  );
}

export function Spinner({ className = "size-4" }: { className?: string }) {
  return <span role="status" aria-label="Loading" className={cx("inline-block animate-spin rounded-full border-2 border-line-strong border-t-fg", className)} />;
}

type Hold = { add: () => () => void };
const Holding = createContext<Hold | null>(null);

/**
 * Keeps its content invisible behind one spinner until nothing inside is
 * loading, then shows it in a single frame, so a page never assembles itself
 * card by card under the reader. It reveals once: anything that loads later
 * shows its own Pending in place. A boundary inside another holds the outer
 * one until it reveals. Content stays laid out while hidden, so charts and
 * scrollers measure their real width. An element marked data-load-visible
 * (the page's title bar) shows throughout.
 */
export function LoadBoundary({ children, className }: { children: ReactNode; className?: string }) {
  const count = useRef(0);
  const [, recount] = useState(0);
  const [shown, setShown] = useState(false);
  const shownRef = useRef(false);
  const [hold] = useState<Hold>(() => ({
    add: () => {
      if (shownRef.current) return () => {};
      count.current++;
      return () => {
        count.current--;
        recount((n) => n + 1);
      };
    },
  }));
  useLoading(!shown);
  useLayoutEffect(() => {
    if (!shownRef.current && count.current === 0) {
      shownRef.current = true;
      setShown(true);
    }
  });
  return (
    <Holding.Provider value={hold}>
      <div className={cx("relative", className)}>
        <div className={cx("contents", !shown && "invisible [&_[data-load-visible]]:visible")} aria-busy={!shown || undefined}>
          {children}
        </div>
        {!shown && (
          <div className="pointer-events-none absolute inset-x-0 top-0 grid h-full max-h-96 min-h-40 animate-[appear_0s_200ms_both] place-items-center">
            <Spinner />
          </div>
        )}
      </div>
    </Holding.Provider>
  );
}

/** Holds the nearest LoadBoundary while pending is true, for data a component shows without a Pending of its own. */
export function useLoading(pending: boolean) {
  const hold = useContext(Holding);
  useLayoutEffect(() => (pending && hold ? hold.add() : undefined), [pending, hold]);
}

/** Stands in for content whose data is loading. Inside a LoadBoundary that has not revealed yet, it holds the boundary. */
export function Pending() {
  useLoading(true);
  return (
    <div className="grid h-40 place-items-center">
      <Spinner />
    </div>
  );
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return "Something went wrong.";
}

export function ErrorNote({ error }: { error: unknown }) {
  if (!error) return null;
  return (
    <p role="alert" className="flex items-start gap-1.5 rounded-control bg-danger-soft px-2.5 py-1.5 text-xs text-danger">
      <Icon name="alert" className="mt-px size-3.5" />
      <span className="min-w-0 [overflow-wrap:anywhere]">{errorMessage(error)}</span>
    </p>
  );
}

/**
 * Opens and closes a native <dialog> as a modal, which traps focus and closes
 * on Escape. React focuses autoFocus elements before the dialog is shown, which
 * does nothing, so the element marked data-autofocus is focused once it is.
 */
export function useModal(open: boolean) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      d.showModal();
      d.querySelector<HTMLElement>("[data-autofocus]")?.focus();
    }
    if (!open && d.open) d.close();
  }, [open]);
  return ref;
}

/**
 * Content mounts on the first opening and stays mounted while the dialog
 * animates out. Each opening remounts it, so forms start empty every time.
 */
export function Dialog({ open, onClose, title, children, wide }: { open: boolean; onClose: () => void; title: string; children: ReactNode; wide?: boolean }) {
  const ref = useModal(open);
  const [opening, setOpening] = useState(open ? 1 : 0);
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setOpening(opening + 1);
  }
  return (
    <dialog
      ref={ref}
      onClose={onClose}
      onClick={(e) => e.target === ref.current && onClose()}
      aria-label={title}
      className={cx(
        "mx-auto mt-[8dvh] mb-0 max-h-[calc(92dvh-1rem)] w-[calc(100%-2rem)] overflow-hidden rounded-overlay border-0 bg-surface p-0 text-fg shadow-floating",
        wide ? "max-w-xl" : "max-w-md",
      )}
    >
      {opening > 0 && (
        <div key={opening} className="flex max-h-[calc(92dvh-1rem)] flex-col">
          <div className="flex shrink-0 items-center justify-between gap-4 border-b border-line py-2.5 pr-2.5 pl-4">
            <h2 className="min-w-0 truncate text-base font-semibold">{title}</h2>
            <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close" icon>
              <Icon name="x" />
            </Button>
          </div>
          <LoadBoundary className="flex min-h-0 flex-col">{children}</LoadBoundary>
        </div>
      )}
    </dialog>
  );
}

export function DialogBody({ children }: { children: ReactNode }) {
  return <div className="relative min-h-0 overflow-auto p-4">{children}</div>;
}

export function Card({ children, className }: { children: ReactNode; className?: string }) {
  return <section className={cx("rounded-surface border border-line bg-surface", className)}>{children}</section>;
}

/** A card's title and description, with its controls beside them on wide screens and beneath them on phones. */
export function CardHeader({ title, description, children }: { title: ReactNode; description?: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex flex-col gap-2 px-4 py-3 md:flex-row md:items-center md:gap-4">
      <div className="min-w-0 flex-1">
        <h2 className="text-sm font-semibold">{title}</h2>
        {description && <p className="text-xs text-muted">{description}</p>}
      </div>
      {children && <div className="flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  );
}

/**
 * Horizontal scrolling for a table wider than its card. An edge that hides
 * content fades and carries a button that scrolls one view along, so the
 * overflow shows even where scrollbars are hidden. A cell marked data-sticky
 * stays put, and the left button sits beside it.
 */
export function ScrollX({ children, startAtEnd, label }: { children: ReactNode; startAtEnd?: boolean; label: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [edge, setEdge] = useState({ left: false, right: false, sticky: 0 });
  useLayoutEffect(() => {
    const el = ref.current!;
    if (startAtEnd) el.scrollLeft = el.scrollWidth;
    const update = () => {
      const sticky = el.querySelector<HTMLElement>("[data-sticky]")?.offsetWidth ?? 0;
      const left = el.scrollLeft > 1;
      const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
      setEdge((e) => (e.left === left && e.right === right && e.sticky === sticky ? e : { left, right, sticky }));
    };
    update();
    el.addEventListener("scroll", update, { passive: true });
    const ro = new ResizeObserver(update);
    ro.observe(el);
    if (el.firstElementChild) ro.observe(el.firstElementChild);
    return () => {
      el.removeEventListener("scroll", update);
      ro.disconnect();
    };
  }, [startAtEnd]);
  const by = (dir: 1 | -1) => ref.current?.scrollBy({ left: dir * Math.max(120, (ref.current.clientWidth - edge.sticky) * 0.8), behavior: "smooth" });
  const nudge = "pointer-events-auto absolute top-1.5 hidden size-7 md:grid place-items-center rounded-full bg-surface text-muted shadow-floating transition-colors hover:text-fg";
  return (
    <div className="relative" data-scrolled={edge.left || undefined}>
      <div ref={ref} role="region" aria-label={label} tabIndex={0} className="relative overflow-x-auto overscroll-x-contain">
        {children}
      </div>
      <div aria-hidden={!edge.left} className={cx("pointer-events-none absolute inset-y-0 w-8 bg-gradient-to-r from-surface transition-opacity", edge.left ? "opacity-100" : "opacity-0")} style={{ left: edge.sticky }}>
        {edge.left && (
          <button type="button" tabIndex={-1} aria-label={`Scroll ${label} left`} onClick={() => by(-1)} className={cx(nudge, "left-1")}>
            <Icon name="left" className="size-3.5" />
          </button>
        )}
      </div>
      <div aria-hidden={!edge.right} className={cx("pointer-events-none absolute inset-y-0 right-0 w-10 bg-gradient-to-l from-surface transition-opacity", edge.right ? "opacity-100" : "opacity-0")}>
        {edge.right && (
          <button type="button" tabIndex={-1} aria-label={`Scroll ${label} right`} onClick={() => by(1)} className={cx(nudge, "right-1.5")}>
            <Icon name="right" className="size-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}

/**
 * A table row's one control, laid over the whole row (the row must be
 * relative), so the row is the target while controls in other cells stay
 * clickable above it. It sits in a cell that is not sticky, since a sticky
 * cell would contain it. It takes focus when clicked, so the drawer it opens
 * hands focus back to it on closing.
 */
export function RowButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={(e) => {
        e.currentTarget.focus();
        onClick();
      }}
      className="absolute inset-0 z-[2] focus-visible:-outline-offset-2"
    />
  );
}

export function EmptyState({ icon, title, children, action }: { icon?: IconName; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-surface border border-dashed border-line-strong px-4 py-10 text-center">
      {icon && <Icon name={icon} className="mb-1 size-5 text-faint" />}
      <p className="text-base font-semibold">{title}</p>
      {children && <p className="max-w-sm text-sm text-balance text-muted">{children}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

/** Set by the app shell on phones, where the page header carries the menu button. */
export const NavMenu = createContext<(() => void) | null>(null);

export function PageHeader({ title, children }: { title: ReactNode; children?: ReactNode }) {
  const openNav = useContext(NavMenu);
  return (
    <header data-load-visible className="flex h-12 shrink-0 items-center gap-3 border-b border-line px-3 md:px-5">
      {openNav && (
        <Button variant="ghost" size="sm" onClick={openNav} aria-label="Open menu" icon className="-ml-1">
          <Icon name="menu" />
        </Button>
      )}
      <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{title}</h1>
      {children && <div className="flex shrink-0 items-center gap-1.5">{children}</div>}
    </header>
  );
}

export function Crumbs({ parent, to, current }: { parent: string; to: string; current: string }) {
  return (
    <span className="flex min-w-0 items-center gap-2">
      <Link to={to} className="shrink-0 font-normal text-muted transition-colors hover:text-fg">
        {parent}
      </Link>
      <span className="text-faint" aria-hidden="true">
        /
      </span>
      <span className="min-w-0 truncate" aria-current="page">
        {current}
      </span>
    </span>
  );
}

export function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const m = matchMedia(query);
      m.addEventListener("change", onChange);
      return () => m.removeEventListener("change", onChange);
    },
    () => matchMedia(query).matches,
  );
}

/** Tailwind's md breakpoint. Below it the app is laid out for a phone. */
export const useWide = () => useMedia("(min-width: 48rem)");
