import type { Metadata } from "next";
import { Setup } from "../Setup";

export const metadata: Metadata = { title: "New workspace" };

export default function NewWorkspacePage() {
  return <Setup later />;
}
