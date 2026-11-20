"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { api, type Hub } from "./api";

/** Loads public hub info for sign-in pages; sends visitors to first-run setup when needed. */
export function useHub(): Hub | null {
  const router = useRouter();
  const [hub, setHub] = useState<Hub | null>(null);
  useEffect(() => {
    let live = true;
    api<Hub>("GET", "/hub").then(
      (h) => {
        if (!live) return;
        if (h.setup_required) router.replace("/hub-setup");
        else setHub(h);
      },
      () => {},
    );
    return () => {
      live = false;
    };
  }, [router]);
  return hub;
}
