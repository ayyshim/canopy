"use client";

import { useState } from "react";
import { api, type Me } from "@/lib/api";
import { useMe } from "@/lib/me";
import { activeSnooze, pausedLabel, SNOOZE_CHOICES } from "@/lib/snooze";

/** The pause that is running for this person, as a label, or "" when not paused. */
export function usePausedLabel(): string {
  const { me } = useMe();
  const now = new Date();
  const until = activeSnooze(me.user.snooze_until, now);
  return until ? pausedLabel(until, now) : "";
}

/** Choices to pause push notifications, or resume them. */
export function PauseNotifications({ onDone }: { onDone?: () => void }) {
  const { me, setMe } = useMe();
  const [error, setError] = useState("");
  const label = usePausedLabel();

  async function set(until: Date | null) {
    setError("");
    try {
      setMe(await api<Me>("PATCH", "/me", { snooze_until: until ? until.toISOString() : null }));
      onDone?.();
    } catch {
      setError("Couldn’t save. Try again.");
    }
  }

  return (
    <div className="stack-sm">
      {label && <p role="status"><b>{label}</b></p>}
      <div className="row wrap">
        {SNOOZE_CHOICES.map((c) => <button key={c.id} className="btn btn-sm" onClick={() => set(c.until(new Date()))}>{c.label}</button>)}
        {label && <button className="btn btn-sm btn-primary" onClick={() => set(null)}>Resume</button>}
      </div>
      {error && <span className="err" role="status">{error}</span>}
    </div>
  );
}
