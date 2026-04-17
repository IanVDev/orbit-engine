import type { Metadata } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";
import Header from "@/components/Header";
import Footer from "@/components/Footer";

const inter = Inter({
  variable: "--font-inter",
  subsets: ["latin"],
  display: "swap",
});

const jetbrains = JetBrains_Mono({
  variable: "--font-jetbrains",
  subsets: ["latin"],
  display: "swap",
});

export const metadata: Metadata = {
  metadataBase: new URL("https://orbit.dev"),
  title: {
    default: "Orbit — Operational visibility for AI coding sessions",
    template: "%s · Orbit",
  },
  description:
    "Orbit watches how you work with Claude Code, GPT, and Gemini. Detects waste patterns, records every decision, and shows you exactly what changed. Evidence, not estimates.",
  keywords: [
    "AI observability",
    "AI coding",
    "Claude Code",
    "developer tools",
    "session telemetry",
    "AI workflow",
  ],
  openGraph: {
    title: "Orbit — Operational visibility for AI coding sessions",
    description:
      "The observability layer for AI coding workflows. Detect waste patterns, record every decision.",
    type: "website",
  },
  twitter: {
    card: "summary_large_image",
    title: "Orbit — Operational visibility for AI coding sessions",
    description:
      "The observability layer for AI coding workflows. Detect waste patterns, record every decision.",
  },
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html
      lang="en"
      className={`${inter.variable} ${jetbrains.variable} h-full antialiased`}
    >
      <body className="min-h-full flex flex-col bg-bg text-text">
        <Header />
        <main className="flex-1">{children}</main>
        <Footer />
      </body>
    </html>
  );
}
