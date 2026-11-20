import type { Metadata } from "next";
import { Setup } from "./Setup";

export const metadata: Metadata = { title: "Set up a workspace" };

export default function SetupPage() {
  return <Setup />;
}
