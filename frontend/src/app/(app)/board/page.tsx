import type { Metadata } from "next";
import { Board } from "./Board";

export const metadata: Metadata = { title: "Board" };

export default function BoardPage() {
  return <Board />;
}
