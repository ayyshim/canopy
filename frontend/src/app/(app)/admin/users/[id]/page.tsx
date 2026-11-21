import type { Metadata } from "next";
import { Suspense } from "react";
import { UserDetail } from "./UserDetail";

export const metadata: Metadata = { title: "User" };

export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function AdminUserPage() {
  return (
    <Suspense>
      <UserDetail />
    </Suspense>
  );
}
