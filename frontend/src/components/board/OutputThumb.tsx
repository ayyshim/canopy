"use client";

import { useState } from "react";
import type { OutputView } from "@/lib/types";
import { Thumb } from "./icons";

/** OutputThumb shows the newest version of an image output; other kinds get their icon. */
export function OutputThumb({ o }: { o: OutputView }) {
  const [failed, setFailed] = useState("");
  const v = o.versions[o.versions.length - 1];
  const src = v && v.mime.startsWith("image/") ? `/api/v1/outputs/${o.id}/versions/${v.number}/file` : "";
  if (!src || failed === src) return <Thumb kind={o.kind} />;
  // eslint-disable-next-line @next/next/no-img-element -- files come from the hub, not the Next image loader
  return <img className="othumb" src={src} alt="" loading="lazy" decoding="async" draggable={false} onError={() => setFailed(src)} />;
}
