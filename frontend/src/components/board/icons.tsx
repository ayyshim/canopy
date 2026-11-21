// Small inline icons and output thumbnails used on the board.
import { initials } from "@/lib/api";
import { isPrototype } from "@/lib/prototype";

export function Brand({ size = 24 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 28 28" aria-hidden="true">
      <rect x="9" y="2" width="10" height="7" rx="2" fill="#16794F" />
      <path d="M14 9v4M6 13h16M6 13v4M14 13v4M22 13v4" stroke="currentColor" strokeWidth="1.6" fill="none" strokeLinecap="round" />
      <rect x="2" y="17" width="8" height="8" rx="2" fill="#FFE58A" />
      <rect x="10" y="17" width="8" height="8" rx="2" fill="#BFE6D1" />
      <rect x="18" y="17" width="8" height="8" rx="2" fill="#D9D0FA" />
    </svg>
  );
}

export const Chevron = () => (
  <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
    <path d="M3 4.5L6 7.5 9 4.5" stroke="currentColor" strokeWidth="1.6" fill="none" />
  </svg>
);

export const GridIcon = () => (
  <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
    {[[2, 2], [9, 2], [2, 9], [9, 9]].map(([x, y]) => (
      <rect key={`${x}${y}`} x={x} y={y} width="5" height="5" rx="1.2" stroke="currentColor" strokeWidth="1.5" fill="none" />
    ))}
  </svg>
);

export const LogoutIcon = () => (
  <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
    <path d="M6 3H3.5A1.5 1.5 0 0 0 2 4.5v7A1.5 1.5 0 0 0 3.5 13H6M10.5 11l3-3-3-3M13.5 8H6" stroke="currentColor" strokeWidth="1.6" fill="none" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);

export const DotsIcon = () => (
  <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true">
    <circle cx="4" cy="9" r="1.6" fill="currentColor" />
    <circle cx="9" cy="9" r="1.6" fill="currentColor" />
    <circle cx="14" cy="9" r="1.6" fill="currentColor" />
  </svg>
);

export const LockIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16">
    <rect x="3" y="7" width="10" height="7" rx="2" fill="#fff" />
    <path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" stroke="#fff" strokeWidth="1.8" fill="none" />
  </svg>
);

export const EyeIcon = () => (
  <svg width="15" height="15" viewBox="0 0 16 16">
    <path d="M1.5 8s2.5-4.5 6.5-4.5S14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" stroke="#fff" strokeWidth="1.6" fill="none" />
    <circle cx="8" cy="8" r="2" fill="#fff" />
  </svg>
);

export const RepoIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
    <circle cx="4" cy="4" r="1.8" stroke="currentColor" strokeWidth="1.5" fill="none" />
    <circle cx="4" cy="12" r="1.8" stroke="currentColor" strokeWidth="1.5" fill="none" />
    <circle cx="12" cy="6" r="1.8" stroke="currentColor" strokeWidth="1.5" fill="none" />
    <path d="M4 5.8v4.4M12 7.8c0 2.2-3 2.2-6.4 3.4" stroke="currentColor" strokeWidth="1.5" fill="none" />
  </svg>
);

export const KIND_LABEL: Record<string, string> = { image: "Image", recording: "Recording", document: "Document", diff: "Diff", report: "Report" };

/** kindLabel is the type shown on cards and in the viewer; HTML outputs are prototypes. */
export const kindLabel = (o: { kind: string; name: string; versions: { mime: string }[] }) => (isPrototype(o) ? "Prototype" : KIND_LABEL[o.kind]);

export function Thumb({ kind }: { kind: string }) {
  switch (kind) {
    case "prototype":
      return (
        <svg viewBox="0 0 30 30" preserveAspectRatio="xMidYMid slice">
          <rect width="30" height="30" fill="#E8EEFC" /><rect x="3" y="5" width="24" height="20" rx="2.5" fill="#fff" stroke="#3E7BFA" strokeWidth="1.4" />
          <rect x="3" y="5" width="24" height="5" rx="2" fill="#3E7BFA" /><circle cx="6" cy="7.5" r=".9" fill="#fff" /><circle cx="9" cy="7.5" r=".9" fill="#fff" />
          <rect x="6" y="13" width="9" height="3" rx="1" fill="#A3AEBB" /><rect x="6" y="18" width="18" height="2.5" rx="1" fill="#C9D1DC" /><path d="M18 12.5l5 3-2.2.5-1 2z" fill="#14181F" />
        </svg>
      );
    case "image":
      return (
        <svg viewBox="0 0 30 30" preserveAspectRatio="xMidYMid slice">
          <rect width="30" height="30" fill="#E9EEF5" /><rect x="3" y="4" width="24" height="5" rx="1.5" fill="#16794F" />
          <rect x="3" y="12" width="14" height="3" fill="#A3AEBB" /><rect x="3" y="17" width="20" height="3" fill="#A3AEBB" />
          <rect x="17" y="23" width="10" height="4" rx="1" fill="#3E7BFA" />
        </svg>
      );
    case "diff":
      return (
        <svg viewBox="0 0 30 30" preserveAspectRatio="xMidYMid slice">
          <rect width="30" height="30" fill="#0D1117" /><rect x="3" y="5" width="22" height="3" fill="#C4352B" opacity=".8" />
          <rect x="3" y="10" width="18" height="3" fill="#3FA877" /><rect x="3" y="15" width="24" height="3" fill="#3FA877" />
          <rect x="3" y="20" width="14" height="3" fill="#8BA0B8" />
        </svg>
      );
    case "report":
      return (
        <svg viewBox="0 0 30 30" preserveAspectRatio="xMidYMid slice">
          <rect width="30" height="30" fill="#fff" /><rect x="5" y="16" width="4" height="9" fill="#8E5BD9" />
          <rect x="11" y="10" width="4" height="15" fill="#0E9AA7" /><rect x="17" y="13" width="4" height="12" fill="#E8743B" />
          <rect x="23" y="6" width="4" height="19" fill="#4C5FD5" />
        </svg>
      );
    case "recording":
      return (
        <svg viewBox="0 0 30 30" preserveAspectRatio="xMidYMid slice">
          <rect width="30" height="30" fill="#14181F" /><path d="M12 10l9 5-9 5z" fill="#fff" />
        </svg>
      );
    default:
      return (
        <svg viewBox="0 0 30 30" preserveAspectRatio="xMidYMid slice">
          <rect width="30" height="30" fill="#fff" /><rect x="5" y="5" width="16" height="3" fill="#14181F" />
          <rect x="5" y="11" width="20" height="2" fill="#A3AEBB" /><rect x="5" y="15" width="18" height="2" fill="#A3AEBB" />
          <rect x="5" y="19" width="20" height="2" fill="#A3AEBB" /><rect x="5" y="23" width="12" height="2" fill="#A3AEBB" />
        </svg>
      );
  }
}

export function Avatar({ name, color, size = 38, square }: { name: string; color: string; size?: number; square?: boolean }) {
  return (
    <span className={"avatar" + (square ? " sq" : "")} aria-hidden="true"
      style={{ background: color, width: size, height: size, fontSize: Math.max(9, Math.round(size * 0.36)) }}>
      {initials(name)}
    </span>
  );
}
