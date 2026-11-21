"use client";

import { useState } from "react";

/** ImageView fits an image inside the stage. A click shows it at actual size, and scrolls if it is larger. */
export function ImageView({ src, alt }: { src: string; alt: string }) {
  const [actual, setActual] = useState(false);
  // Outputs are files from the hub, so next/image's optimizer doesn't apply.
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      className={`stage-img ${actual ? "actual" : "fit"}`}
      src={src}
      alt={alt}
      tabIndex={0}
      title={actual ? "Click to fit the window" : "Click for actual size"}
      onClick={() => setActual((x) => !x)}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); setActual((x) => !x); } }}
    />
  );
}
