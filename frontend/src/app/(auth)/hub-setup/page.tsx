import type { Metadata } from "next";
import { Suspense } from "react";
import { HubSetupForm } from "./HubSetupForm";

export const metadata: Metadata = { title: "Set up this hub" };

export default function HubSetupPage() {
  return (
    <Suspense>
      <HubSetupForm />
    </Suspense>
  );
}
