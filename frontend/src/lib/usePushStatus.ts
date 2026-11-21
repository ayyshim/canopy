"use client";

import { useCallback, useEffect, useState } from "react";
import { browserEnv, pushStatus, subscribePush, unsubscribePush, type PushStatus } from "./push";

/** The push state of this device, with actions to turn it on or off. */
export function usePushStatus() {
  const [status, setStatus] = useState<PushStatus | null>(null);

  const refresh = useCallback(async () => {
    try {
      setStatus(await pushStatus(browserEnv()));
    } catch {
      setStatus({ support: "unsupported", permission: "unsupported", subscribed: false });
    }
  }, []);

  useEffect(() => {
    // Wait a tick so the state isn't set while the effect runs.
    const t = setTimeout(refresh, 0);
    return () => clearTimeout(t);
  }, [refresh]);

  const turnOn = useCallback(async () => {
    await subscribePush(browserEnv());
    await refresh();
  }, [refresh]);
  const turnOff = useCallback(async () => {
    await unsubscribePush(browserEnv());
    await refresh();
  }, [refresh]);

  return { status, refresh, turnOn, turnOff };
}
