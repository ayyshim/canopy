import type { Metadata } from "next";
import { Suspense } from "react";
import { SetupForm } from "./SetupForm";

export const metadata: Metadata = { title: "Set up two-step sign-in" };

export default function TwoFactorSetupPage() {
  return (
    <Suspense>
      <SetupForm />
    </Suspense>
  );
}
