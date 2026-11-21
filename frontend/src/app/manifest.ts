import type { MetadataRoute } from "next";

// Colors match --paper and --green in globals.css.
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Canopy",
    short_name: "Canopy",
    description: "See and steer every coding agent in one live tree.",
    start_url: "/",
    scope: "/",
    display: "standalone",
    background_color: "#F6F7F9",
    theme_color: "#16794F",
    icons: [
      { src: "/icons/icon-192.png", sizes: "192x192", type: "image/png", purpose: "any" },
      { src: "/icons/icon-512.png", sizes: "512x512", type: "image/png", purpose: "any" },
      { src: "/icons/icon-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  };
}
