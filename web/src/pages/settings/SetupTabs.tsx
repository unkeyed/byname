import { useState } from "react";
import { Link } from "react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../../api";
import { BrandForm } from "../../config/BrandForm";
import { BrandTable } from "../../config/BrandTable";
import { OtherBrandsSettings } from "../../config/OtherBrandsSettings";
import { CredentialForm, ProviderKeyList, testTone, useCredentials } from "../../config/Credentials";
import { QuestionEditor } from "../../config/QuestionEditor";
import { RunSettingsForm } from "../../config/RunSettings";
import { WherePeopleAsk as Surfaces } from "../../config/WherePeopleAsk";
import { appCount, checkPrice, scheduleLine } from "../../checks";
import { money } from "../../money";
import { keys, useCan, useCosts, useSettings } from "../../queries";
import { Badge, Button, Card, CardHeader, cx, ErrorNote, Icon, Pending, RelativeTime, SteadyLabel } from "../../ui";
import { RunNowButton } from "../results/RunNow";
import { SettingsHeader, SettingsSubhead } from "./Settings";

// Settings pages over the same components onboarding uses, so each setup screen exists once.

export function General() {
  return (
    <>
      <SettingsHeader title="General" />
      <BrandForm retention />
    </>
  );
}

export function Competitors() {
  return (
    <>
      <SettingsHeader title="Competitors" />
      <div className="space-y-6">
        <BrandTable />
        <OtherBrandsSettings />
      </div>
    </>
  );
}

export function Questions() {
  return (
    <>
      <SettingsHeader title="Questions" />
      <QuestionEditor history />
    </>
  );
}

export function WherePeopleAsk() {
  return (
    <>
      <SettingsHeader title="AI apps" />
      <Surfaces />
    </>
  );
}

/** Settings > Treg: the one token that pays for every AI app's answers. */
export function Treg() {
  const qc = useQueryClient();
  const list = useCredentials();
  const [editing, setEditing] = useState(false);
  const test = useMutation({ mutationFn: () => api.credentials.test("treg"), onSuccess: () => qc.invalidateQueries({ queryKey: keys.credentials }) });
  const t = list.data?.find((c) => c.kind === "treg");
  return (
    <>
      <SettingsHeader title="Treg" />
      {list.isPending ? (
        <Pending />
      ) : list.isError ? (
        <ErrorNote error={list.error} />
      ) : t ? (
        <Card>
          <CardHeader
            title={
              <span className="flex flex-wrap items-center gap-2">
                Treg token
                {t.set ? <Badge tone="success">Saved</Badge> : <Badge tone="warn">Not set</Badge>}
                {t.set && t.org && <Badge tone="outline">organization {t.org}</Badge>}
                {t.last_test_status && <Badge tone={testTone(t.last_test_status)}>{t.last_test_status === "ok" ? "Works" : "Test failed"}</Badge>}
              </span>
            }
            description={
              t.set ? (
                <>
                  {t.updated_at && (
                    <>
                      Saved <RelativeTime iso={t.updated_at} />.{" "}
                    </>
                  )}
                  {t.last_tested_at && (
                    <>
                      Tested <RelativeTime iso={t.last_tested_at} />.
                    </>
                  )}
                </>
              ) : (
                "No AI app can be asked until a token is saved."
              )
            }
          >
            {t.set && (
              <Button size="sm" variant="ghost" onClick={() => test.mutate()} disabled={test.isPending}>
                <SteadyLabel labels={["Test it", "Testing…"]} current={test.isPending ? "Testing…" : "Test it"} />
              </Button>
            )}
            {t.set && (
              <Button size="sm" aria-expanded={editing} onClick={() => setEditing(!editing)}>
                Replace
              </Button>
            )}
          </CardHeader>
          {(!t.set || editing) && (
            <div className="border-t border-line px-4 py-3">
              <CredentialForm kind="treg" title="Treg" set={t.set} org={t.org} onDone={t.set ? () => setEditing(false) : undefined} />
              <p className="mt-2 text-xs text-muted">
                Get a token at{" "}
                <a href="https://treg.to" target="_blank" rel="noreferrer" className="font-medium text-fg underline-offset-2 hover:underline">
                  treg.to
                </a>
                . A team token works on its own. A personal token also needs your Treg organization name.
              </p>
            </div>
          )}
          {t.last_test_status && t.last_test_status !== "ok" && <p className="border-t border-line px-4 py-3 text-xs text-danger [overflow-wrap:anywhere]">{t.last_test_status}</p>}
          {test.error != null && (
            <div className="px-4 pb-3">
              <ErrorNote error={test.error} />
            </div>
          )}
        </Card>
      ) : null}
    </>
  );
}

export function ProviderKeys() {
  return (
    <>
      <SettingsHeader title="Provider keys" />
      <ProviderKeyList />
    </>
  );
}

/** Settings > Schedule and limits: when checks run, what they and social searches cost, and the caps. */
export function ScheduleAndSpending() {
  return (
    <>
      <SettingsHeader title="Schedule and limits" />
      <div className="space-y-8">
        <CheckSchedule />
        <MonthSpend />
        <section>
          <SettingsSubhead>Schedule and spend caps</SettingsSubhead>
          <RunSettingsForm schedule limits tuningCollapsed />
        </section>
      </div>
    </>
  );
}

/** When the next check runs, with Pause and Check now. */
function CheckSchedule() {
  const qc = useQueryClient();
  const settings = useSettings();
  const canRun = useCan("runs:write");
  const toggle = useMutation({
    mutationFn: (pause: boolean) => (pause ? api.checks.pause() : api.checks.resume()),
    onSuccess: (v) => qc.setQueryData(keys.settings, v),
  });
  if (settings.isPending) return <Pending />;
  if (settings.isError) return <ErrorNote error={settings.error} />;
  const s = settings.data;
  const last = s.last_check;
  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            AI checks {s.checks_paused ? <Badge tone="warn">Paused</Badge> : <Badge tone="success">On schedule</Badge>}
          </span>
        }
        description={
          <>
            {scheduleLine(s)}{" "}
            {last ? (
              <>
                Last check{" "}
                <Link to="/settings/checks" className="font-medium text-fg hover:underline">
                  #{last.run_id}
                </Link>{" "}
                <RelativeTime iso={last.started_at ?? last.queued_at} />.
              </>
            ) : (
              "No check has run yet."
            )}
          </>
        }
      >
        {canRun && (
          <>
            <Button size="sm" onClick={() => toggle.mutate(!s.checks_paused)} disabled={toggle.isPending}>
              <Icon name={s.checks_paused ? "refresh" : "x"} className="size-3.5" />
              <SteadyLabel labels={["Pause", "Resume"]} current={s.checks_paused ? "Resume" : "Pause"} />
            </Button>
            <RunNowButton variant="secondary" />
          </>
        )}
      </CardHeader>
      {toggle.error != null && (
        <div className="px-4 pb-3">
          <ErrorNote error={toggle.error} />
        </div>
      )}
    </Card>
  );
}

/** This month's spend against the cap, and what a check and a month are expected to cost. */
export function MonthSpend() {
  const costs = useCosts();
  if (costs.isPending) return <Pending />;
  if (costs.isError) return <ErrorNote error={costs.error} />;
  const c = costs.data;
  const m = c.month;
  const price = checkPrice(c);
  const share = m.cap_usd ? Math.min(1, m.spent_usd / m.cap_usd) : null;
  const e = c.checks.check;
  return (
    <Card>
      <CardHeader title="This month" description="Spent so far in this calendar month, against your monthly spend cap. Expected figures are estimates." />
      <div className="space-y-4 border-t border-line px-4 py-3 text-sm">
        <div>
          <p>
            <span className="text-2xl font-semibold tabular-nums">{money(m.spent_usd)}</span>
            <span className="text-muted"> spent{m.cap_usd != null ? ` of your ${money(m.cap_usd)} cap` : ", with no monthly cap"}</span>
          </p>
          {share != null && (
            <div className="mt-2 h-2 rounded-full bg-subtle" role="meter" aria-valuemin={0} aria-valuemax={m.cap_usd!} aria-valuenow={m.spent_usd} aria-label="Spent of the monthly cap">
              <div className={cx("h-2 rounded-full", share >= 0.9 ? "bg-danger" : share >= 0.7 ? "bg-warn" : "bg-fg")} style={{ width: `${Math.max(share * 100, 1)}%` }} />
            </div>
          )}
          <p className="mt-1 text-xs text-muted">
            {money(m.checks_usd)} on checks, {money(m.social_usd)} on social searches and {money(m.brands_usd)} on finding other brands and their logos.
            {m.missing_cost_calls > 0 && ` ${m.missing_cost_calls} answers reported no price, so the real spend is a little higher.`}
          </p>
        </div>
        <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[auto_1fr]">
          <dt className="text-muted">One check</dt>
          <dd className="first-letter:uppercase">
            {price.amount}: {e.questions} questions in {appCount(c)} AI apps{e.samples > 1 && `, ${e.samples} times each`}, {e.calls} answers
            {e.find_brands ? `, with ${money(e.find_brands.cost_usd)} to read them for other brands.` : "."}
            {price.unknown && <span className="text-muted"> {price.unknown}</span>}
          </dd>
          <dt className="text-muted">Expected this month</dt>
          <dd className="first-letter:uppercase">
            {money(c.month_estimate_usd, "about")}:{" "}
            {c.checks.paused ? "no scheduled checks while paused" : `${money(c.checks.month_usd)} for a check every ${c.checks.check_every_days === 1 ? "day" : `${c.checks.check_every_days} days`}`}
            {c.social.enabled ? `, plus ${money(c.social.month_usd)} for social searches` : ", with social searches off"}.
          </dd>
        </dl>
      </div>
    </Card>
  );
}
