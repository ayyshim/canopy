type Out = { id: string; name: string; task_id?: string | null; versions: { mime: string }[] };

/** isPrototype is true for HTML outputs, which open as live pages. */
export function isPrototype(o: { name: string; versions: { mime: string }[] }): boolean {
  const mime = o.versions[o.versions.length - 1]?.mime ?? "";
  return /^text\/html/i.test(mime) || /\.html?$/i.test(o.name);
}

/** pageName turns the name a page reports into an output name: no query, hash or leading slash. */
export function pageName(raw: string): string {
  return raw.split(/[?#]/)[0].replace(/^(\.\/|\/)+/, "");
}

/** findOutputByName finds the output a page link points to: same task first, then the rest of the goal. */
export function findOutputByName<T extends Out>(outputs: T[], current: T, raw: string, goalOf: (taskId: string) => string | undefined): T | undefined {
  const name = pageName(raw);
  if (!name) return undefined;
  const same = outputs.find((o) => o.name === name && o.task_id === current.task_id);
  if (same) return same;
  const goal = current.task_id ? goalOf(current.task_id) : undefined;
  if (!goal) return undefined;
  return outputs.find((o) => o.name === name && !!o.task_id && goalOf(o.task_id) === goal);
}
