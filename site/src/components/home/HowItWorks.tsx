import Section from "@/components/Section";
import Card from "@/components/Card";
import CTAButton from "@/components/CTAButton";

const steps = [
  {
    n: "01",
    title: "Install once.",
    body: "Drop the skill into your AI assistant. 30 seconds.",
  },
  {
    n: "02",
    title: "Work normally.",
    body: "Orbit reads the session as it happens. It never modifies your code.",
  },
  {
    n: "03",
    title: "Get diagnoses on demand.",
    body: "When waste patterns appear, Orbit tells you what, how risky, and what to do next.",
  },
];

export default function HowItWorks() {
  return (
    <Section
      eyebrow="How it works"
      title="Three steps. No dashboards to configure."
      subtitle="No integrations to build. No accounts to create. Install, work, and read the diagnosis when it matters."
    >
      <div className="grid md:grid-cols-3 gap-4 sm:gap-5 mb-10">
        {steps.map((step) => (
          <Card key={step.n} className="relative overflow-hidden">
            <div className="font-mono text-[11px] text-accent tracking-[0.2em] mb-4">
              {step.n}
            </div>
            <h3 className="text-[17px] font-semibold text-text mb-2">
              {step.title}
            </h3>
            <p className="text-[14px] text-text-2 leading-relaxed">{step.body}</p>
          </Card>
        ))}
      </div>

      <div className="rounded-[var(--radius-lg)] border border-border bg-[color:var(--color-mono-bg)] p-4 sm:p-5 flex items-center justify-between gap-4">
        <code className="font-mono text-[13px] text-text overflow-x-auto no-scrollbar">
          <span className="text-text-3">$</span> drag <span className="text-accent">skill/</span> into Claude Code
        </code>
        <CTAButton href="/docs/quickstart" variant="secondary" className="h-9 px-3 text-[12px]">
          Read the full guide →
        </CTAButton>
      </div>
    </Section>
  );
}
