"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { api, ApiError, landingPath, type Hub, type Me } from "@/lib/api";

/** First run: hub setup. Signed in: the person's landing page. Otherwise: sign in. */
export function HomeRedirect() {
  const router = useRouter();
  useEffect(() => {
    (async () => {
      const hub = await api<Hub>("GET", "/hub");
      if (hub.setup_required) return router.replace("/hub-setup");
      try {
        router.replace(landingPath(await api<Me>("GET", "/me")));
      } catch (e) {
        router.replace(e instanceof ApiError && e.status === 401 ? "/login" : "/login?error=oauth_failed");
      }
    })().catch(() => router.replace("/login"));
  }, [router]);
  return <p className="sr-only" role="status">Loading…</p>;
}
