import Link from "next/link";

export function Logo({ href = "/" }: { href?: string }) {
  return (
    <Link href={href} className="logo" aria-label="Canopy home">
      <span className="logo-mark" aria-hidden="true">
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
          <circle cx="8" cy="4" r="2.2" fill="#3FA877" />
          <circle cx="3.5" cy="11" r="2" fill="#BFE6D1" />
          <circle cx="12.5" cy="11" r="2" fill="#BFE6D1" />
          <path d="M8 6.2v2.3M8 8.5 3.8 9.2M8 8.5l4.2.7" stroke="#BFE6D1" strokeWidth="1.2" strokeLinecap="round" />
        </svg>
      </span>
      Canopy
    </Link>
  );
}
