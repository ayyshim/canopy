/** Choices for “Pause notifications”. Times are in the browser’s time zone. */

export type SnoozeChoice = { id: "hour" | "tomorrow"; label: string; until: (now: Date) => Date };

export const SNOOZE_CHOICES: SnoozeChoice[] = [
  { id: "hour", label: "For 1 hour", until: (now) => new Date(now.getTime() + 3600_000) },
  { id: "tomorrow", label: "Until 8:00 tomorrow", until: tomorrowAt8 },
];

/** 8:00 the next morning, local time. */
export function tomorrowAt8(now: Date): Date {
  const d = new Date(now);
  d.setDate(d.getDate() + 1);
  d.setHours(8, 0, 0, 0);
  return d;
}

/** The pause that is still running, or null. */
export function activeSnooze(until: string | null | undefined, now: Date): Date | null {
  if (!until) return null;
  const d = new Date(until);
  return d.getTime() > now.getTime() ? d : null;
}

/** “Paused until 3:40 pm” today, “Paused until tomorrow 8:00 am”, or the weekday for later. */
export function pausedLabel(until: Date, now: Date): string {
  const time = until.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((day(until) - day(now)) / 86400_000);
  if (days <= 0) return `Paused until ${time}`;
  if (days === 1) return `Paused until tomorrow ${time}`;
  return `Paused until ${until.toLocaleDateString([], { weekday: "long" })} ${time}`;
}
