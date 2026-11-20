import type { CSSProperties } from "react";
import { initials } from "@/lib/api";

/** A logo image, or the name's initials when there is none. */
export function LogoMark({ name, url, size = 28, style }: { name: string; url?: string | null; size?: number; style?: CSSProperties }) {
  const box: CSSProperties = { width: size, height: size, ...style };
  if (url) {
    // eslint-disable-next-line @next/next/no-img-element
    return <img className="wsi wsi-img" src={url} alt="" style={box} />;
  }
  return <span className="wsi" style={{ ...box, fontSize: Math.max(10, Math.round(size * 0.4)), ...style }}>{initials(name)}</span>;
}
