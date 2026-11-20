import type { Metadata } from "next";
import { WorkspaceBoard } from "./WorkspaceBoard";

export const metadata: Metadata = { title: "Board" };

export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function WorkspacePage() {
  return <WorkspaceBoard />;
}
