import type { Metadata } from "next";
import { Organizations } from "./Organizations";

export const metadata: Metadata = { title: "Organizations" };

export default function AdminOrganizationsPage() {
  return <Organizations />;
}
