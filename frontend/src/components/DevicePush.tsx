"use client";

import { useCallback, useEffect, useState } from "react";
import { ApiError } from "@/lib/api";
import { currentEnv, currentSubscription, pushAvailability, sendTestPush, turnOffPush, turnOnPush, type PushAvailability } from "@/lib/devicePush";

type State = PushAvailability | "loading" | "on" | "off";

const STATUS: Record<Exclude<State, "loading">, string> = {
  on: "Push is on for this device.",
  off: "Push is off for this device.",
  blocked: "Your browser blocks notifications from Canopy. Allow them in the site settings, then turn push on.",
  unsupported: "This browser can’t show push notifications.",
  install: "Add Canopy to your Home Screen to get notifications. Buttons in notifications don’t show on iPhone and iPad.",
  ready: "Push is off for this device.",
};

/** “This device”: turn push on or off for this browser and send a test. */
export function DevicePush() {
  const [state, setState] = useState<State>("loading");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");

  const refresh = useCallback(async () => {
    const a = pushAvailability(currentEnv());
    if (a !== "ready") return setState(a);
    setState((await currentSubscription().catch(() => null)) ? "on" : "off");
  }, []);
  useEffect(() => { void refresh(); }, [refresh]);

  async function run(fn: () => Promise<string | void>) {
    setBusy(true);
    setMsg("");
    try {
      const done = await fn();
      if (done) setMsg(done);
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : "Something went wrong. Try again.");
    }
    setBusy(false);
    await refresh();
  }

  const can = state === "on" || state === "off" || state === "ready";
  return (
    <div className="note-box stack-sm" aria-labelledby="this-device">
      <b id="this-device">This device</b>
      <span className="hint" role="status">{state === "loading" ? "Checking…" : STATUS[state]}</span>
      <div className="row wrap">
        {can && state !== "on" && (
          <button className="btn btn-sm btn-primary" disabled={busy} onClick={() => run(async () => (await turnOnPush()) ? undefined : "Permission wasn’t given, so push stays off.")}>Turn on push</button>
        )}
        {state === "on" && <button className="btn btn-sm" disabled={busy} onClick={() => run(turnOffPush)}>Turn off</button>}
        {state === "on" && <button className="btn btn-sm" disabled={busy} onClick={() => run(async () => { await sendTestPush(); return "Test sent. It should arrive in a moment."; })}>Send a test</button>}
      </div>
      {msg && <span className="hint" role="status">{msg}</span>}
    </div>
  );
}
