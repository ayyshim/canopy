"use client";

import { createContext, useContext } from "react";
import { useBoard } from "@/lib/board/store";
import type { Board } from "@/lib/types";

/** Who is looking at which workspace; set once by the board page. */
export const BoardCtx = createContext<{ wsId: string; me: string; send: (type: string, data: unknown) => void }>({
  wsId: "",
  me: "",
  send: () => {},
});

export const useBoardCtx = () => useContext(BoardCtx);

/** useB returns the loaded board. Components under the board page only render once it exists. */
export function useB(): Board {
  const b = useBoard((s) => s.board);
  if (!b) throw new Error("board not loaded");
  return b;
}
