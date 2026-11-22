import { ChannelPicker } from "../pages/settings/Notifications";
import { useIsAdmin } from "../queries";
import { Icon } from "../ui";

/** Notification channels are admin-only, so other roles see where alerts go instead of a picker they cannot use. */
export function AlertsSlot() {
  const admin = useIsAdmin();
  return (
    <div>
      <h3 className="mb-1.5 text-xs font-medium text-muted">Alerts</h3>
      {admin ? (
        <ChannelPicker />
      ) : (
        <p className="flex items-start gap-2 rounded-surface border border-dashed border-line-strong px-3 py-2.5 text-sm text-muted">
          <Icon name="mail" className="mt-0.5 size-4" />
          Alerts tell you what changed after each check. They show in the app, and an admin can also send them by email, to Slack or to a webhook from Settings.
        </p>
      )}
    </div>
  );
}
