import type { Metadata } from "next";
import { NoWorkspace } from "./NoWorkspace";

export const metadata: Metadata = { title: "No workspace yet" };

export default function NoWorkspacePage() {
  return <NoWorkspace />;
}
