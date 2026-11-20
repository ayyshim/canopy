import type { Metadata } from "next";
import { HubSettingsForm } from "./HubSettingsForm";

export const metadata: Metadata = { title: "Hub settings" };

export default function AdminSettingsPage() {
  return <HubSettingsForm />;
}
