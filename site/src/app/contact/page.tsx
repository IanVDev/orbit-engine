import type { Metadata } from "next";
import Section from "@/components/Section";
import Card from "@/components/Card";
import CTAButton from "@/components/CTAButton";

export const metadata: Metadata = {
  title: "Contact",
  description: "Talk to the Orbit team about teams, self-hosting, or security.",
};

const reasons = [
  {
    title: "For teams",
    body:
      "Interested in Orbit Pro for your engineering team? We'll walk through how aggregated patterns work without exposing individual sessions.",
    email: "teams@orbit.dev",
    label: "teams@orbit.dev",
  },
  {
    title: "Security & compliance",
    body:
      "Security review, responsible disclosure, or SOC-style questionnaires. We answer every one within 48 hours.",
    email: "security@orbit.dev",
    label: "security@orbit.dev",
  },
  {
    title: "Self-hosting & enterprise",
    body:
      "Air-gapped deployments, SSO, custom schemas, SLAs. Let us know your constraints and we'll propose the shortest path.",
    email: "enterprise@orbit.dev",
    label: "enterprise@orbit.dev",
  },
  {
    title: "General",
    body:
      "Everything else — partnerships, press, feedback. Response times vary but we read everything.",
    email: "hello@orbit.dev",
    label: "hello@orbit.dev",
  },
];

export default function ContactPage() {
  return (
    <>
      <Section
        eyebrow="Contact"
        title="Reach the right person, faster."
        subtitle="Pick the closest match. We reply from a real address, not a ticketing bot."
      />
      <Section>
        <div className="grid md:grid-cols-2 gap-4 sm:gap-5">
          {reasons.map((r) => (
            <Card key={r.title}>
              <h3 className="text-[16px] font-semibold text-text mb-2">{r.title}</h3>
              <p className="text-[14px] text-text-2 leading-relaxed mb-4">{r.body}</p>
              <a
                href={`mailto:${r.email}`}
                className="inline-flex items-center gap-2 font-mono text-[13px] text-accent hover:underline"
              >
                {r.label} →
              </a>
            </Card>
          ))}
        </div>
      </Section>

      <Section>
        <div className="rounded-[var(--radius-lg)] border border-accent/30 bg-accent/5 p-8 sm:p-10 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-5">
          <div>
            <h2 className="text-2xl font-semibold text-text mb-2">
              Prefer to try it first?
            </h2>
            <p className="text-text-2">Install the free skill and send questions once you&apos;ve seen a diagnosis.</p>
          </div>
          <CTAButton href="/docs/quickstart" variant="primary">
            Install the free skill
          </CTAButton>
        </div>
      </Section>
    </>
  );
}
