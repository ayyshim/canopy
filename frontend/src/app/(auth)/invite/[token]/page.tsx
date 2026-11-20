import type { Metadata } from "next";
import { Suspense } from "react";
import { Invite } from "./Invite";

export const metadata: Metadata = { title: "Invitation" };

export function generateStaticParams() {
  return [{ token: "_" }];
}

export default function InvitePage() {
  return <Suspense><Invite /></Suspense>;
}
