/** Done and stopped goals are both finished; stopped ones were cut short (a cancelled job run). */
export const goalClosed = (g: { status: string } | undefined | null): boolean => g?.status === "done" || g?.status === "stopped";
