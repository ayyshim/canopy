"use client";

import Link from "next/link";
import { useCallback, useEffect } from "react";
import { computer, computerFor, signInLabel } from "@/lib/board/derive";
import { useDesktopNotifications } from "@/lib/board/notify";
import { useAppBadge, useCloseResolvedNotifications, useDeepLink } from "@/lib/board/pushSync";
import { useBoardSocket } from "@/lib/board/socket";
import { useBoard } from "@/lib/board/store";
import { useUI } from "@/lib/board/ui";
import { isWorkspaceActive } from "@/lib/liveStatus";
import { useMe } from "@/lib/me";
import { usePushStatus } from "@/lib/usePushStatus";
import { useLiveTab } from "@/lib/useLiveTab";
import { currentOrg } from "@/lib/api";
import { Canvas } from "./Canvas";
import { Chats } from "./chat/ChatWindow";
import { BoardCtx, useB, useBoardCtx } from "./context";
import { Dialogs } from "./dialogs/Dialogs";
import { Tray } from "./outputs/Tray";
import { Viewer } from "./outputs/Viewer";
import { Panel } from "./panels/Panel";
import { TopBar } from "./TopBar";

/** BoardApp is one workspace's live board. */
export function BoardApp({ wsId }: { wsId: string }) {
  const { me } = useMe();
  const sendRef = useBoardSocket(wsId);
  const board = useBoard((s) => s.board);
  const gone = useBoard((s) => s.gone);
  const send = useCallback((type: string, data: unknown) => sendRef.current(type, data), [sendRef]);
  const { status: push } = usePushStatus();
  useDesktopNotifications(me, push?.subscribed === true);
  useAppBadge(me);
  useCloseResolvedNotifications(me);
  useDeepLink(wsId, !!board);
  useLiveTab(currentOrg(me)?.name, board?.workspace.name ?? "", !!board && isWorkspaceActive(board));

  useEffect(() => {
    try { localStorage.setItem("canopy-last-ws", wsId); } catch { /* storage blocked */ }
    document.body.classList.add("on-board");
    return () => document.body.classList.remove("on-board");
  }, [wsId]);

  if (gone) {
    return (
      <div className="loading">
        <div className="empty card">
          <h2>{gone === "deleted" ? "This workspace was deleted" : "You no longer have access to this workspace"}</h2>
          <Link className="btn" href="/board">Go to your workspaces</Link>
        </div>
      </div>
    );
  }
  if (!board) return <div className="loading" role="status">Loading the board…</div>;
  return (
    <BoardCtx.Provider value={{ wsId, me: me.user.id, send }}>
      <div id="app">
        <TopBar />
        <Main />
      </div>
      <Viewer />
      <Dialogs />
      <Toasts />
    </BoardCtx.Provider>
  );
}

function Main() {
  const side = useUI((s) => s.side);
  const tray = useUI((s) => s.tray);
  useEffect(() => {
    document.body.classList.toggle("tray-open", tray);
  }, [tray]);
  return (
    <div className={"main" + (side ? " side-open" : "")}>
      {/* Overlays come first so keyboard and screen reader users reach them before the tree. */}
      <Panel />
      <Chats />
      <Tray />
      <Banners />
      <Canvas />
    </div>
  );
}

/** Banners say when your goals have nowhere to run, and what staff can see. */
function Banners() {
  const b = useB();
  const { me } = useBoardCtx();
  const own = b.computers[me];
  const runsOn = computerFor(b, me);
  const staff = b.viewer.role === "staff";
  return (
    <>
      {!own && (
        <div className="banner warn staffbanner" style={{ top: staff ? 62 : 12 }}>
          {runsOn ? <>Your goals and chats run on <b>{computer(b, runsOn)?.name}</b> (shared, {signInLabel(b, runsOn)}).</> : "Connect a computer to run your goals and chats."}{" "}
          <Link className="linkbtn" href="/org/computers">Connect your computer</Link>
        </div>
      )}
      {staff && <div className="banner warn staffbanner">You’re staff in this workspace, so you only see the managers and staff you’ve been given.</div>}
    </>
  );
}

export function Toasts() {
  const toasts = useUI((s) => s.toasts);
  const drop = useUI((s) => s.dropToast);
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast ${t.kind === "error" ? "bad" : t.kind === "ok" ? "" : t.kind}`}>
          <span className="spacer">{t.text}</span>
          <button onClick={() => drop(t.id)} aria-label="Dismiss">✕</button>
        </div>
      ))}
    </div>
  );
}
