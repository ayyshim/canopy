"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, ApiError, type Me } from "./api";
import { unauthorizedPath } from "./twoFactor";

type MeState = { me: Me; setMe: (me: Me) => void; refresh: () => Promise<void> };

const MeContext = createContext<MeState | null>(null);

/** Loads the signed-in user for app pages; sends signed-out visitors to /login. */
export function MeProvider({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const [me, setMe] = useState<Me | null>(null);
  const [failed, setFailed] = useState(false);

  const load = useCallback(
    () =>
      api<Me>("GET", "/me").then(setMe, (e) => {
        if (e instanceof ApiError && e.status === 401) router.replace(unauthorizedPath(e));
        else setFailed(true);
      }),
    [router],
  );

  useEffect(() => {
    let live = true;
    api<Me>("GET", "/me").then(
      (m) => live && setMe(m),
      (e) => {
        if (!live) return;
        if (e instanceof ApiError && e.status === 401) router.replace(unauthorizedPath(e));
        else setFailed(true);
      },
    );
    return () => {
      live = false;
    };
  }, [router]);

  const refresh = load;

  if (failed) {
    return (
      <div className="center-page">
        <div className="empty card">
          <h2>We can’t reach Canopy</h2>
          <p className="muted">Check your connection and try again.</p>
          <button className="btn" onClick={() => location.reload()}>Try again</button>
        </div>
      </div>
    );
  }
  if (!me) return <p className="sr-only" role="status">Loading…</p>;
  return <MeContext.Provider value={{ me, setMe, refresh }}>{children}</MeContext.Provider>;
}

export function useMe(): MeState {
  const ctx = useContext(MeContext);
  if (!ctx) throw new Error("useMe outside MeProvider");
  return ctx;
}
