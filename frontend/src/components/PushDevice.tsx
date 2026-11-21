"use client";

import { useState } from "react";
import { browserEnv, IOS_BUTTONS_NOTE, IOS_INSTALL_NOTE, isIOS, PushError, testPush } from "@/lib/push";
import { usePushStatus } from "@/lib/usePushStatus";

/** Push notifications for this browser or phone: turn on or off, and send a test. */
export function PushDevice() {
  const { status, turnOn, turnOff } = usePushStatus();
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);

  async function run(fn: () => Promise<unknown>, done = "") {
    setBusy(true);
    setMsg("");
    try {
      await fn();
      setMsg(done);
    } catch (e) {
      setMsg(e instanceof PushError || e instanceof Error ? e.message : "That didn’t work. Try again.");
    } finally {
      setBusy(false);
    }
  }

  if (!status) return null;
  const ios = typeof navigator !== "undefined" && isIOS(navigator);
  return (
    <div className="stack" style={{ marginTop: 12 }}>
      <b>Push on this device</b>
      {status.support === "needs-install" && <span className="hint">{IOS_INSTALL_NOTE}</span>}
      {status.support === "unsupported" && <span className="hint">This browser can’t receive push notifications.</span>}
      {status.support === "ok" && (
        <div className="row wrap">
          {status.subscribed ? (
            <>
              <button className="btn" disabled={busy} onClick={() => run(turnOff, "Push is off for this device.")}>Turn off</button>
              <button className="btn" disabled={busy} onClick={() => run(async () => { const n = await testPush(browserEnv()); setMsg(`Sent to ${n} device${n === 1 ? "" : "s"}.`); })}>Send a test</button>
            </>
          ) : (
            <button className="btn" disabled={busy || status.permission === "denied"} onClick={() => run(turnOn, "Push is on for this device.")}>Turn on push</button>
          )}
        </div>
      )}
      {status.permission === "denied" && <span className="err">Notifications are blocked. Allow them in the site settings, then come back.</span>}
      {ios && <span className="hint">{IOS_BUTTONS_NOTE}</span>}
      {status.subscribed && !ios && <span className="hint">Questions and approvals show buttons. You can answer without opening Canopy.</span>}
      {msg && <span role="status" className="hint">{msg}</span>}
    </div>
  );
}
