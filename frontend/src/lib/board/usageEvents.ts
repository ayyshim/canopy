type HasUsage = { id: string; usage?: unknown };

/** Task and goal events carry no usage; keep the totals the board already has. */
export function keepUsage<T extends HasUsage>(next: T, list: T[]): T {
  if (next.usage) return next;
  const old = list.find((x) => x.id === next.id);
  return old?.usage ? { ...next, usage: old.usage } : next;
}

/** A usage.work event sets the new totals on one task and its goal. */
export function applyUsageWork<T extends HasUsage, G extends HasUsage>(
  tasks: T[], goals: G[], d: { task_id?: string; task?: unknown; goal_id?: string; goal?: unknown },
): { tasks: T[]; goals: G[] } {
  return {
    tasks: d.task_id && d.task ? tasks.map((t) => (t.id === d.task_id ? { ...t, usage: d.task } : t)) : tasks,
    goals: d.goal_id && d.goal ? goals.map((g) => (g.id === d.goal_id ? { ...g, usage: d.goal } : g)) : goals,
  };
}
