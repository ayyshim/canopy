"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

export function SentMessage() {
  const email = useSearchParams().get("email");
  return (
    <>
      <p>
        If an account uses {email ? <strong>{email}</strong> : "that email"}, we sent it a link to reset the password.
        The link works once and expires in 30 minutes.
      </p>
      <p className="hint"><Link href="/login">Back to sign in</Link></p>
    </>
  );
}
