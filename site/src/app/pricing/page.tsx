import type { Metadata } from "next";
import Section from "@/components/Section";
import Card from "@/components/Card";
import CTAButton from "@/components/CTAButton";

export const metadata: Metadata = {
  title: "Pricing",
  description: "Free forever for individuals. Pro for teams. Enterprise and self-hosting available.",
};

const free = [
  "Full waste pattern detection (all 6 patterns)",
  "Real-time diagnosis with risk levels",
  "Specific action recommendations",
  "Silent when the session is healthy",
  "Works with Claude Code, GPT, Gemini",
  "No account, no login, no tracking",
];

const pro = [
  "Everything in Free",
  "Session history across all sessions",
  "Impact tracking (before/after metrics)",
  "Team dashboard with aggregate patterns",
  "Evolution engine with automated quality gates",
  "Append-only audit trail, exportable",
];

const enterprise = [
  "Everything in Pro",
  "Self-hosted deployment",
  "HMAC-signed tracking channel",
  "SSO and audit integrations",
  "Custom schema extensions",
  "Priority response SLA",
];

export default function PricingPage() {
  return (
    <>
      <Section
        eyebrow="Pricing"
        title="Free for individuals. Paid for teams."
        subtitle="No seat fees on the free tier. Pay when aggregation, team views, and evidence retention become load-bearing."
      />

      <Section>
        <div className="grid md:grid-cols-3 gap-4 sm:gap-5 items-stretch">
          <Card className="flex flex-col">
            <div className="font-mono text-[11px] uppercase tracking-[0.16em] text-text-3 mb-4">
              Free
            </div>
            <div className="mb-4">
              <span className="text-4xl font-semibold text-text">$0</span>
              <span className="text-text-3 text-sm ml-2">forever</span>
            </div>
            <p className="text-[14px] text-text-2 mb-5 leading-relaxed">
              Everything you need for personal use. Local only.
            </p>
            <ul className="space-y-2 mb-6 flex-1">
              {free.map((f) => (
                <li key={f} className="text-[13.5px] text-text-2 flex items-start gap-2">
                  <span className="text-healthy mt-[6px]">●</span>
                  <span>{f}</span>
                </li>
              ))}
            </ul>
            <CTAButton href="/docs/quickstart" variant="secondary">
              Install the free skill
            </CTAButton>
          </Card>

          <Card className="flex flex-col border-accent/40 bg-accent/[0.04] relative">
            <span className="absolute -top-3 left-6 px-2 py-0.5 rounded-full bg-accent text-[10px] uppercase tracking-[0.14em] text-[#0a0c14] font-mono font-semibold">
              Recommended
            </span>
            <div className="font-mono text-[11px] uppercase tracking-[0.16em] text-accent mb-4">
              Pro
            </div>
            <div className="mb-4">
              <span className="text-4xl font-semibold text-text">Contact</span>
              <span className="text-text-3 text-sm ml-2">pricing TBD</span>
            </div>
            <p className="text-[14px] text-text-2 mb-5 leading-relaxed">
              For developers and teams who want to measure and improve over time.
            </p>
            <ul className="space-y-2 mb-6 flex-1">
              {pro.map((f) => (
                <li key={f} className="text-[13.5px] text-text-2 flex items-start gap-2">
                  <span className="text-accent mt-[6px]">●</span>
                  <span>{f}</span>
                </li>
              ))}
            </ul>
            <CTAButton href="/contact" variant="primary">
              Talk to us
            </CTAButton>
          </Card>

          <Card className="flex flex-col">
            <div className="font-mono text-[11px] uppercase tracking-[0.16em] text-text-3 mb-4">
              Enterprise
            </div>
            <div className="mb-4">
              <span className="text-4xl font-semibold text-text">Custom</span>
            </div>
            <p className="text-[14px] text-text-2 mb-5 leading-relaxed">
              Self-hosting, signing, SSO, and SLAs. Air-gapped deployments possible.
            </p>
            <ul className="space-y-2 mb-6 flex-1">
              {enterprise.map((f) => (
                <li key={f} className="text-[13.5px] text-text-2 flex items-start gap-2">
                  <span className="text-text-3 mt-[6px]">●</span>
                  <span>{f}</span>
                </li>
              ))}
            </ul>
            <CTAButton href="/contact" variant="secondary">
              Contact sales
            </CTAButton>
          </Card>
        </div>
      </Section>

      <Section>
        <div className="rounded-[var(--radius-lg)] border border-border bg-surface/40 p-6 text-[14px] text-text-2 leading-relaxed">
          <p className="mb-2 text-text font-semibold">Pricing philosophy.</p>
          Orbit Free exists to prove the value of the product on a single developer&apos;s
          machine. You do not need a payment method to use it. Pro exists because
          aggregating evidence across a team, retaining history, and running
          evolution safely has real operational cost. Everything we charge for is a
          feature that costs us money to run, not a feature we held hostage.
        </div>
      </Section>
    </>
  );
}
