import type { Metadata } from "next";
import { NewOrgForm } from "./NewOrgForm";

export const metadata: Metadata = { title: "Create an organization" };

export default function NewOrgPage() {
  return <NewOrgForm />;
}
