"use client";

import { useEffect, useRef } from "react";

/** Calls onEvent when a job or one of its runs changes. Reconnects after a drop. */
export function useJobEvents(wsId: string, onEvent: (type: string, data: { id?: string; job_id?: string }) => void) {
  const cb = useRef(onEvent);
  useEffect(() => { cb.current = onEvent; });
  useEffect(() => {
    if (!wsId || wsId === "_") return;
    let ws: WebSocket | null = null;
    let stopped = false;
    let retry = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const connect = () => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      ws = new WebSocket(`${proto}://${location.host}/ws/workspaces/${wsId}`);
      ws.onopen = () => { retry = 0; };
      ws.onmessage = (e) => {
        try {
          const m = JSON.parse(e.data) as { type: string; data: { id?: string; job_id?: string } };
          if (m.type === "job.updated" || m.type === "job.run.updated") cb.current(m.type, m.data ?? {});
        } catch { /* not for us */ }
      };
      ws.onclose = () => {
        if (stopped) return;
        retry = Math.min(retry + 1, 6);
        timer = setTimeout(connect, 500 * 2 ** retry);
      };
    };
    connect();
    return () => { stopped = true; clearTimeout(timer); ws?.close(); };
  }, [wsId]);
}
