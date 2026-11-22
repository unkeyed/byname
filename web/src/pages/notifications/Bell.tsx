import { NavLink } from "react-router";
import { useNotifications } from "../../queries";
import { buttonClass, cx, Icon } from "../../ui";

/**
 * The notifications bell: a link to the notifications page with the unread
 * count. The badge keeps its size and place whatever the count, and is only
 * hidden at zero, so a count arriving or changing never moves anything.
 */
export function Bell({ onClick, className }: { onClick?: () => void; className?: string }) {
  const unread = useNotifications().data?.unread ?? 0;
  const label = unread > 0 ? `Notifications, ${unread} unread` : "Notifications";
  return (
    <NavLink
      to="/notifications"
      onClick={onClick}
      aria-label={label}
      title={label}
      className={({ isActive }) => cx(buttonClass({ variant: "ghost", size: "sm", icon: true }), "relative", isActive && "bg-subtle-strong text-fg", className)}
    >
      <Icon name="bell" />
      <span
        aria-hidden="true"
        className={cx(
          "absolute top-1 right-0.5 grid h-4 w-5 place-items-center rounded-full bg-danger text-[0.625rem] leading-none font-semibold text-surface tabular-nums ring-2 ring-bg md:-top-1.5 md:-right-2",
          unread === 0 && "invisible",
        )}
      >
        {unread > 9 ? "9+" : unread}
      </span>
    </NavLink>
  );
}
