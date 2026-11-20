import type { Metadata } from "next";
import { Suspense } from "react";
import { ResetForm } from "./ResetForm";

export const metadata: Metadata = { title: "Choose a new password" };

// Exported once; the hub serves every /reset/<token> from it.
export function generateStaticParams() {
  return [{ token: "_" }];
}

export default function ResetPage() {
  return (
    <Suspense>
      <ResetForm />
    </Suspense>
  );
}
