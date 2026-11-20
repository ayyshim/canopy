import type { Metadata } from "next";
import { Suspense } from "react";
import { SentMessage } from "./SentMessage";

export const metadata: Metadata = { title: "Check your email" };

export default function SentPage() {
  return (
    <div className="authcard">
      <h1>Check your email</h1>
      <Suspense fallback={<p className="muted">If an account uses that email, we sent it a reset link.</p>}>
        <SentMessage />
      </Suspense>
    </div>
  );
}
