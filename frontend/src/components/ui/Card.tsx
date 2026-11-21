import type { ReactNode } from "react";

/** Card is a bordered surface. With a title it gets a header row with an optional action on the right. */
export function Card({ title, sub, action, children, danger, className }: {
  title?: ReactNode; sub?: ReactNode; action?: ReactNode; children?: ReactNode; danger?: boolean; className?: string;
}) {
  return (
    <section className={"ui-card" + (danger ? " is-danger" : "") + (className ? " " + className : "")}>
      {(title || action) && <SectionHeader title={title} sub={sub} action={action} />}
      {children}
    </section>
  );
}

/** SectionHeader is a title, at most one muted line under it, and an optional action. */
export function SectionHeader({ title, sub, action }: { title?: ReactNode; sub?: ReactNode; action?: ReactNode }) {
  return (
    <div className="ui-sh">
      <div className="ui-sh-t">
        {title && <h2>{title}</h2>}
        {sub && <p className="hint">{sub}</p>}
      </div>
      {action && <div className="ui-sh-a">{action}</div>}
    </div>
  );
}
