import type { NextConfig } from "next";

// Rewrites use this value at startup; restart Next.js after changing API_ORIGIN.
const configuredOrigin = process.env.API_ORIGIN ?? "http://localhost:8080";
const apiURL = new URL(configuredOrigin);

if (
  !["http:", "https:"].includes(apiURL.protocol) ||
  apiURL.username !== "" ||
  apiURL.password !== "" ||
  apiURL.pathname !== "/" ||
  apiURL.search !== "" ||
  apiURL.hash !== ""
) {
  throw new Error("API_ORIGIN must be an http(s) origin without credentials or a path");
}

const nextConfig: NextConfig = {
  distDir: process.env.NEXT_DIST_DIR ?? ".next",
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
  async rewrites() {
    return [
      {
        source: "/api/v1/healthz",
        destination: `${apiURL.origin}/healthz`,
      },
      {
        source: "/api/v1/:path*",
        destination: `${apiURL.origin}/api/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
