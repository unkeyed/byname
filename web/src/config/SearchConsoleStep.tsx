import type { ReactNode } from "react";
import { Badge, Card, cx, labelClass, selectClass } from "../ui";

/**
 * Onboarding step 5, until the Search Console connection exists: its fields,
 * described and disabled. The Search Console workstream replaces this file.
 */
export function SearchConsoleStep({ footer }: { footer: ReactNode }) {
  return (
    <div className="space-y-5">
      <Card className="space-y-5 p-4">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="text-sm font-semibold">Google Search Console</h3>
          <Badge>Coming soon</Badge>
        </div>
        <p className="text-sm text-muted">
          Connecting Search Console adds clicks, impressions and position from Google, read-only. You can connect it later in Settings &gt; Search Console, so skip this
          step for now.
        </p>
        <fieldset disabled className="space-y-4 opacity-70">
          <div>
            <span className={labelClass}>Connection method</span>
            <div className="grid gap-2 sm:grid-cols-2">
              {[
                ["Sign in with Google", "Read-only Search Console access, through this instance's own Google OAuth client."],
                ["Service account", "A JSON key, for self-hosters who prefer one."],
              ].map(([title, detail], i) => (
                <label key={title} className={cx("flex gap-2.5 rounded-surface border p-3", i === 0 ? "border-line-hover" : "border-line-strong")}>
                  <input type="radio" name="gsc-method" defaultChecked={i === 0} className="mt-0.5" />
                  <span>
                    <span className="block text-sm font-medium">{title}</span>
                    <span className="block text-xs text-muted">{detail}</span>
                  </span>
                </label>
              ))}
            </div>
          </div>
          <label className="block">
            <span className={labelClass}>Property</span>
            <select className={cx(selectClass, "w-full")}>
              <option>Listed from the account once connected</option>
            </select>
            <span className="mt-1 block text-xs text-faint">A domain property (sc-domain:) or a URL-prefix property. The one matching your primary domain is preselected.</span>
          </label>
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" defaultChecked className="mt-0.5" />
            <span>
              Backfill history
              <span className="block text-xs text-muted">Pulls as much history as the API returns.</span>
            </span>
          </label>
        </fieldset>
      </Card>
      {footer}
    </div>
  );
}
