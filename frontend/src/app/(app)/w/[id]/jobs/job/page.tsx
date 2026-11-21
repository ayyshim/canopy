import type { Metadata } from "next";
import { JobPage } from "./JobPage";

export const metadata: Metadata = { title: "Job" };

export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function Page() {
  return <JobPage />;
}
