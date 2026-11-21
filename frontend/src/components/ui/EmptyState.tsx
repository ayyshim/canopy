import type { ReactNode } from "react";

/** EmptyState says what goes here and offers one action. */
export function EmptyState({ icon = "○", children, action }: { icon?: ReactNode; children: ReactNode; action?: ReactNode }) {
  return (
    <div className="ui-empty">
      <span className="ui-empty-i" aria-hidden="true">{icon}</span>
      <p className="muted">{children}</p>
      {action}
    </div>
  );
}
