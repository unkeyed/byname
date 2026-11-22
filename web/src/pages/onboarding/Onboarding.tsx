import { useState, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, useLocation, useNavigate, useParams } from "react-router";
import { api, ApiError, type OnboardingStep, type StepKey } from "../../api";
import { keys, useCan, useMe, useOnboarding } from "../../queries";
import { useTheme } from "../../theme";
import { Button, buttonClass, cx, Dialog, EmptyState, ErrorNote, Icon, LoadBoundary, Logo, Spinner, useWide } from "../../ui";
import { AlertsSlot } from "../../config/AlertsSlot";
import { BrandForm } from "../../config/BrandForm";
import { BrandTable } from "../../config/BrandTable";
import { QuestionEditor } from "../../config/QuestionEditor";
import { RunSettingsForm } from "../../config/RunSettings";
import { SearchConsoleStep } from "../../config/SearchConsoleStep";
import { SocialStep } from "../../config/SocialStep";
import { WherePeopleAsk } from "../../config/WherePeopleAsk";
import { ReauthProvider } from "../settings/Reauth";
import { Review } from "./Review";

/** What a step view needs to move on: where Back goes, and marking the step saved, which also moves to the next one. */
export interface StepNav {
  step: OnboardingStep;
  back?: string;
  done: () => void;
  pending: boolean;
  error: unknown;
}

/** The buttons under a step: Back, Skip for optional steps, and the step's own primary action. */
export function StepFooter({ nav, skip, children }: { nav: StepNav; skip?: boolean; children?: ReactNode }) {
  return (
    <div className="space-y-2 border-t border-line pt-4">
      <div className="flex flex-wrap items-center gap-2">
        {nav.back && (
          <Link to={nav.back} className={buttonClass({ variant: "ghost" })}>
            <Icon name="left" className="size-3.5" />
            Back
          </Link>
        )}
        <span className="ml-auto" />
        {skip && (
          <Button variant="ghost" onClick={nav.done} disabled={nav.pending}>
            Skip
          </Button>
        )}
        {children}
      </div>
      <ErrorNote error={nav.error} />
    </div>
  );
}

const continueLabel = "Save and continue";

const views: Record<StepKey, { intro: string; render: (nav: StepNav) => ReactNode }> = {
  brand: {
    intro: "Who you are, so Byname can tell when an AI answer names you or links to your site. You can change all of this later in Settings.",
    render: (nav) => <BrandForm onSaved={nav.done} submitLabel={continueLabel} footer={(submit) => <StepFooter nav={nav}>{submit}</StepFooter>} />,
  },
  competitors: {
    intro:
      "The brands buyers pick instead of you. Add the ones you lose deals to, so you see how often AI names them next to you, or instead of you. Byname tracks only the brands you list here, and you can add more any time.",
    render: (nav) => (
      <div className="space-y-5">
        <BrandTable />
        <StepFooter nav={nav} skip>
          <Button variant="primary" onClick={nav.done} disabled={nav.pending}>
            Continue
          </Button>
        </StepFooter>
      </div>
    ),
  },
  questions: {
    intro: "What a buyer might ask an AI before they've heard of you. Each check asks every question in every AI app you pick, so more questions cost more.",
    render: (nav) => <QuestionEditor onSaved={nav.done} submitLabel={continueLabel} footer={(submit) => <StepFooter nav={nav}>{submit}</StepFooter>} />,
  },
  surfaces: {
    intro:
      "Pick where your buyers ask AI. Byname asks each one your questions and records exactly what a person would see. A label on each app says how close Byname gets to the real thing.",
    render: (nav) => <WherePeopleAsk prefillDefaults={!nav.step.saved} onContinue={nav.done} footer={(primary) => <StepFooter nav={nav}>{primary}</StepFooter>} />,
  },
  search_console: {
    intro:
      "Search Console is Google's free report of how often your site shows up in Google results and how many people click. Connect it to see that next to your AI results. It's optional, and you can connect it later.",
    render: (nav) => (
      <SearchConsoleStep
        onPicked={nav.done}
        footer={(picked) => (
          <StepFooter nav={nav} skip={!picked}>
            {picked && (
              <Button variant="primary" onClick={nav.done} disabled={nav.pending}>
                Continue
              </Button>
            )}
          </StepFooter>
        )}
      />
    ),
  },
  social: {
    intro: "Public posts on social networks that name you, gathered in one digest.",
    render: (nav) => <SocialStep onSaved={nav.done} footer={(primary) => <StepFooter nav={nav}>{primary}</StepFooter>} />,
  },
  schedule: {
    intro: "How often Byname checks, and the most a check or a month may spend. Nothing runs until you start the first check on the last step.",
    render: (nav) => (
      <RunSettingsForm schedule limits tuningCollapsed extra={<AlertsSlot />} onSaved={nav.done} submitLabel={continueLabel} footer={(submit) => <StepFooter nav={nav}>{submit}</StepFooter>} />
    ),
  },
  review: {
    intro: "Look everything over, see what it will cost, and start the first check.",
    render: (nav) => <Review nav={nav} />,
  },
};

/** For people who cannot change setup, while nobody has finished it. */
export function NotSetUp() {
  return (
    <div className="mx-auto mt-16 max-w-md px-4">
      <EmptyState icon="settings" title="Byname is not set up yet">
        An admin or editor finishes setup: your brand, your questions and the AI apps to ask. Results show here after the first check.
      </EmptyState>
    </div>
  );
}

/**
 * Seven steps on a page of their own. Opening it resumes at the first step
 * not saved yet; any step can be opened again from the list.
 */
export function Onboarding() {
  useTheme();
  const me = useMe();
  const location = useLocation();
  const canEdit = useCan("config:write");
  const onboarding = useOnboarding();
  if (me.error instanceof ApiError && me.error.status === 401) return <Navigate to={`/login?next=${encodeURIComponent(location.pathname)}`} replace />;
  if (me.isPending || (canEdit && onboarding.isPending)) {
    return (
      <div className="grid h-full place-items-center">
        <Spinner />
      </div>
    );
  }
  if (!canEdit) {
    return (
      <Shell>
        <NotSetUp />
      </Shell>
    );
  }
  if (onboarding.isError || !onboarding.data) {
    return (
      <Shell>
        <div className="mx-auto mt-16 max-w-sm space-y-3 px-4">
          <ErrorNote error={onboarding.error ?? me.error} />
          <Button onClick={() => void onboarding.refetch()}>Try again</Button>
        </div>
      </Shell>
    );
  }
  return (
    <ReauthProvider>
      <Wizard steps={onboarding.data.steps} finished={!!onboarding.data.finished_at} />
    </ReauthProvider>
  );
}

function Shell({ children, aside, header }: { children: ReactNode; aside?: ReactNode; header?: ReactNode }) {
  return (
    <div className="flex h-full flex-col bg-bg">
      <header className="flex h-12 shrink-0 items-center gap-2 border-b border-line px-3 md:px-5">
        <Logo />
        <span className="text-sm font-semibold">Byname</span>
        <span className="text-sm text-faint">/</span>
        <span className="text-sm text-muted">Setup</span>
        <div className="ml-auto flex min-w-0 items-center gap-2">{header}</div>
      </header>
      <div className="flex min-h-0 flex-1">
        {aside}
        <main className="relative min-w-0 flex-1 scroll-pb-36 overflow-auto">{children}</main>
      </div>
    </div>
  );
}

function Wizard({ steps, finished }: { steps: OnboardingStep[]; finished: boolean }) {
  const { step: param } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const wide = useWide();
  const [menu, setMenu] = useState(false);
  const index = steps.findIndex((s) => s.key === param);
  const mark = useMutation({
    mutationFn: (key: StepKey) => api.onboarding.markSaved(key),
    onSuccess: (o, key) => {
      qc.setQueryData(keys.onboarding, o);
      const i = steps.findIndex((s) => s.key === key);
      const next = steps[i + 1];
      if (next) void navigate(`/onboarding/${next.key}`);
    },
  });

  if (index < 0) {
    const resume = steps.find((s) => !s.saved) ?? steps[steps.length - 1];
    return <Navigate to={`/onboarding/${resume.key}`} replace />;
  }
  const step = steps[index];
  const view = views[step.key];
  const nav: StepNav = {
    step,
    back: index > 0 ? `/onboarding/${steps[index - 1].key}` : undefined,
    done: () => mark.mutate(step.key),
    pending: mark.isPending,
    error: mark.error,
  };
  const list = <StepList steps={steps} current={step.key} onPick={() => setMenu(false)} />;

  return (
    <Shell
      header={
        <>
          {!wide && (
            <Button size="sm" variant="ghost" onClick={() => setMenu(true)} aria-label="All steps" className="min-w-0">
              <span className="truncate">
                Step {index + 1} of {steps.length}
              </span>
              <Icon name="down" className="size-3.5" />
            </Button>
          )}
          {finished && (
            <Link to="/" className={buttonClass({ size: "sm" })}>
              Dashboard
            </Link>
          )}
        </>
      }
      aside={wide && <aside className="w-60 shrink-0 overflow-auto border-r border-line px-3 py-6 lg:w-64">{list}</aside>}
    >
      <div className="mx-auto max-w-3xl px-4 py-6 pb-16 md:px-8 md:py-8">
        <p className="text-xs font-medium text-muted">
          Step {index + 1} of {steps.length}
          {!step.required && " · optional"}
        </p>
        <h1 className="mt-1 text-lg font-semibold">{step.title}</h1>
        <p className="mt-1 mb-6 text-sm text-muted">{view.intro}</p>
        <LoadBoundary key={step.key}>{view.render(nav)}</LoadBoundary>
      </div>
      {!wide && (
        <Dialog open={menu} onClose={() => setMenu(false)} title="Setup steps">
          <div className="p-3">{list}</div>
        </Dialog>
      )}
    </Shell>
  );
}

function StepList({ steps, current, onPick }: { steps: OnboardingStep[]; current: StepKey; onPick: () => void }) {
  return (
    <nav aria-label="Setup steps">
      <ol className="space-y-0.5">
        {steps.map((s, i) => {
          const active = s.key === current;
          return (
            <li key={s.key}>
              <Link
                to={`/onboarding/${s.key}`}
                onClick={onPick}
                aria-current={active ? "step" : undefined}
                className={cx("flex items-center gap-2.5 rounded-surface px-2 py-3 text-sm transition-colors md:py-1.5", active ? "bg-subtle text-fg" : "text-muted hover:bg-subtle hover:text-fg")}
              >
                <span
                  className={cx(
                    "grid size-5 shrink-0 place-items-center rounded-full text-[11px] font-semibold tabular-nums",
                    s.saved ? "bg-success-soft text-success" : active ? "bg-primary text-primary-fg" : "border border-line-strong",
                  )}
                >
                  {s.saved ? <Icon name="check" className="size-3" /> : i + 1}
                </span>
                <span className="min-w-0 flex-1">
                  <span className={cx("block truncate", active && "font-medium")}>{s.title}</span>
                  {!s.required && <span className="block text-xs text-faint">Optional</span>}
                </span>
              </Link>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
