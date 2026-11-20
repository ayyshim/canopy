import type { NextConfig } from "next";

// `NEXT_OUTPUT=export` builds the static web app that canopy-hub embeds. Development uses the
// dev server, with /api sent to the hub (Caddy does the same at http://localhost:8000).
const exporting = process.env.NEXT_OUTPUT === "export";
const hub = process.env.HUB_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  // Static export can't prerender partially, so Cache Components is a dev-server feature here;
  // every page loads its data from the hub in the browser either way.
  ...(exporting
    ? { output: "export" as const }
    : {
        cacheComponents: true,
        partialPrefetching: true,
        experimental: {
          // Validate instant navigation only where a segment opts in with `export const instant`.
          instantInsights: { validationLevel: "manual-warning" as const },
        },
        async rewrites() {
          return [{ source: "/api/:path*", destination: `${hub}/api/:path*` }];
        },
      }),
};

export default nextConfig;
