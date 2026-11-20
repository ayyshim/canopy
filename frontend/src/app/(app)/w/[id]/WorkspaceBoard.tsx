"use client";

import { usePathname } from "next/navigation";
import { BoardApp } from "@/components/board/BoardApp";

/** The workspace id comes from the address: the static export serves every id from one page. */
export function WorkspaceBoard() {
  const id = usePathname().split("/")[2] ?? "";
  if (!id || id === "_") return null;
  return <BoardApp wsId={id} />;
}
