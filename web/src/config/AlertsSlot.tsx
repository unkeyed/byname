import { Icon } from "../ui";

/** Where alert channels go once notification channels exist; until then alerts stay in the app. */
export function AlertsSlot() {
  return (
    <div>
      <h3 className="mb-1.5 text-xs font-medium text-muted">Alerts</h3>
      <p className="flex items-start gap-2 rounded-surface border border-dashed border-line-strong px-3 py-2.5 text-sm text-muted">
        <Icon name="mail" className="mt-0.5 size-4" />
        Alerts show in the app until you add a channel in Settings &gt; Notifications.
      </p>
    </div>
  );
}
