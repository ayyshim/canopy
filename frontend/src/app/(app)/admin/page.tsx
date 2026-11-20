import type { Metadata } from "next";
import { Overview } from "./Overview";

export const metadata: Metadata = { title: "Hub overview" };

export default function AdminOverviewPage() {
  return <Overview />;
}
