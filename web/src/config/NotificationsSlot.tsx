import { ChannelPicker } from "../pages/settings/Notifications";
import { useIsAdmin } from "../queries";
import { Icon } from "../ui";

/** Where notifications go is admin-only, so other roles see what notifications are instead of a picker they cannot use. */
export function NotificationsSlot() {
  const admin = useIsAdmin();
  return (
    <div>
      <h3 className="mb-1.5 text-xs font-medium text-muted">Notifications</h3>
      {admin ? (
        <ChannelPicker />
      ) : (
        <p className="flex items-start gap-2 rounded-surface border border-dashed border-line-strong px-3 py-2.5 text-sm text-muted">
          <Icon name="mail" className="mt-0.5 size-4" />
          Notifications tell you what Byname noticed, such as a finished check. They show under the bell, and an admin can also send them by email, to Slack or to a webhook from Settings &gt; Notifications.
        </p>
      )}
    </div>
  );
}
