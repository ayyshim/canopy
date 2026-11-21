/** A done goal's tasks leave the board unless someone shows them again. */
export function tasksHiddenByGoal(g: { status: string; show_tasks: boolean } | undefined): boolean {
  return !!g && g.status === "done" && !g.show_tasks;
}
