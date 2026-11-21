import type { Metadata } from "next";
import { Jobs } from "./Jobs";

export const metadata: Metadata = { title: "Jobs" };

export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function JobsPage() {
  return <Jobs />;
}
