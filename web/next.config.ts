import type { NextConfig } from "next";

// When deploying to GitHub Pages (a project site served from
// https://<user>.github.io/<repo>/), the app lives under a sub-path. The Pages
// workflow sets NEXT_PUBLIC_BASE_PATH=/rinfra so assets and routes resolve
// correctly. Locally and in the normal build it is empty, so nothing changes.
const basePath = process.env.NEXT_PUBLIC_BASE_PATH || "";

const nextConfig: NextConfig = {
  output: "export",
  trailingSlash: true,
  basePath: basePath || undefined,
  images: {
    unoptimized: true,
  },
};

export default nextConfig;
