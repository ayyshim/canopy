"use client";

import { useEffect, useRef } from "react";
import { useBoard } from "./store";
import type { Board, PresenceUser } from "../types";

type Message = { type: string; seq?: number; data: unknown };

/** Keeps the board for a workspace live: a snapshot, then deltas; reconnects with a fresh snapshot. */
export function useBoardSocket(workspaceId: string | null) {
  const sendRef = useRef<(type: string, data: unknown) => void>(() => {});

  useEffect(() => {
    if (!workspaceId) return;
    const store = useBoard.getState();
    store.reset();
    let ws: WebSocket | null = null;
    let stopped = false;
    let retry = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const connect = () => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      ws = new WebSocket(`${proto}://${location.host}/ws/workspaces/${workspaceId}`);
      ws.onopen = () => {
        retry = 0;
        useBoard.getState().setConnected(true);
      };
      ws.onmessage = (e) => {
        const msg = JSON.parse(e.data) as Message;
        if (msg.type === "snapshot") {
          const d = msg.data as { board: Board; presence: PresenceUser[] };
          useBoard.getState().setSnapshot(d.board, d.presence);
        } else {
          useBoard.getState().apply(msg.type, msg.data);
        }
      };
      ws.onclose = () => {
        useBoard.getState().setConnected(false);
        if (stopped || useBoard.getState().gone) return;
        retry = Math.min(retry + 1, 6);
        timer = setTimeout(connect, 500 * 2 ** retry);
      };
    };
    connect();
    sendRef.current = (type, data) => {
      if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type, data }));
    };
    return () => {
      stopped = true;
      clearTimeout(timer);
      ws?.close();
    };
  }, [workspaceId]);

  return sendRef;
}
