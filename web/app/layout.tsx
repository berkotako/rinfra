import type { Metadata } from "next";
import { GeistSans } from "geist/font/sans";
import { GeistMono } from "geist/font/mono";
import "./globals.css";
import AppShell from "./AppShell";

export const metadata: Metadata = {
  title: "RInfra — Operations Platform",
  description:
    "Enterprise red-team and purple-team operations platform for professional offensive security.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      style={
        {
          "--font-geist-sans": GeistSans.style.fontFamily,
          "--font-geist-mono": GeistMono.style.fontFamily,
        } as React.CSSProperties
      }
    >
      <body className={`${GeistSans.variable} ${GeistMono.variable}`}>
        <AppShell>{children}</AppShell>
      </body>
    </html>
  );
}
