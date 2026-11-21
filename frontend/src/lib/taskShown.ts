import { goalClosed } from "./goalStatus.ts";
/** A done goal's tasks leave the board unless someone shows them again. */
export function tasksHiddenByGoal(g: { status: string; show_tasks: boolean } | undefined): boolean {
  return !!g && goalClosed(g) && !g.show_tasks;
}
