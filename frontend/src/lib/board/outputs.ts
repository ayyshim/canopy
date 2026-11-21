import type { Board } from "../types";
import { goalClosed } from "../goalStatus.ts";

export const outputGoal = (b: Board, o: { task_id?: string | null }) => {
  const t = o.task_id ? b.tasks.find((x) => x.id === o.task_id) : undefined;
  return t ? b.goals.find((g) => g.id === t.goal_id) : undefined;
};

/** Outputs whose goal is still on the board (not done). Outputs with no goal are left out. */
export const activeOutputs = (b: Board) =>
  b.outputs.filter((o) => {
    const g = outputGoal(b, o);
    return !!g && !goalClosed(g);
  });

export const unseen = (b: Board) =>
  activeOutputs(b).filter((o) => (b.seen[o.id] ?? 0) < o.versions.length || o.status === "review");
