import type { Metadata } from "next";
import { Suspense } from "react";
import { VerifyForm } from "./VerifyForm";

export const metadata: Metadata = { title: "Two-step sign-in" };

export default function TwoFactorPage() {
  return (
    <Suspense>
      <VerifyForm />
    </Suspense>
  );
}
