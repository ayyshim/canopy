"use client";

import { useState } from "react";
import { api, hardNavigate } from "@/lib/api";

export async function logOut() {
  try {
    await api("POST", "/auth/logout");
  } finally {
    // A full load clears every piece of signed-in state.
    hardNavigate("/login");
  }
}

export function LogoutButton({ className = "btn btn-ghost" }: { className?: string }) {
  const [busy, setBusy] = useState(false);
  return (
    <button
      className={className}
      disabled={busy}
      onClick={() => {
        setBusy(true);
        void logOut();
      }}
    >
      Log out
    </button>
  );
}
