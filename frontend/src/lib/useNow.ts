"use client";

import { useEffect, useState } from "react";

/** useNow is the current time, refreshed every `every` milliseconds. */
export function useNow(every = 30000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), every);
    return () => clearInterval(id);
  }, [every]);
  return now;
}
