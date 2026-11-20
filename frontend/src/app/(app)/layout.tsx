import { Suspense } from "react";
import { MeProvider } from "@/lib/me";

// Signed-in pages wait for the session check (GET /me against the hub) before rendering.
export default function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <Suspense>
      <MeProvider>{children}</MeProvider>
    </Suspense>
  );
}
