import type { NextConfig } from "next";

// When deploying to GitHub Pages (a project site served from
// https://<user>.github.io/<repo>/), the app lives under a sub-path. The Pages
// workflow sets NEXT_PUBLIC_BASE_PATH=/rinfra so assets and routes resolve
// correctly. Locally, on Vercel, and in the normal build it is empty, so the app
// is served from the domain root.
const basePath = process.env.NEXT_PUBLIC_BASE_PATH || "";

// Output mode:
//  - default: static export (`out/`). Required for GitHub Pages, and deploys
//    cleanly to Vercel as a static site (the demo runs entirely on mock data, no
//    backend), matching the build that ships to Pages today.
//  - set NEXT_PUBLIC_SSR=true to build a full Next.js app (SSR / API routes) on
//    Vercel instead.
const staticExport = process.env.NEXT_PUBLIC_SSR !== "true";

const nextConfig: NextConfig = {
  ...(staticExport ? { output: "export" as const } : {}),
  trailingSlash: true,
  basePath: basePath || undefined,
  images: {
    unoptimized: true,
  },
};

export default nextConfig;
