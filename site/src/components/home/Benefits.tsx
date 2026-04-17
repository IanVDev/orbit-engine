import Section from "@/components/Section";
import Card from "@/components/Card";

const benefits = [
  {
    title: "Real-time diagnosis",
    body: "Waste patterns caught during the session, not after the bill.",
  },
  {
    title: "Risk-scored actions",
    body: "Each finding ranked low · medium · high · critical, with the exact next step.",
  },
  {
    title: "Silent when healthy",
    body: "No output means no waste. Orbit doesn't nag.",
  },
  {
    title: "Append-only evidence",
    body: "Every decision logged. Auditable. Never overwritten.",
  },
  {
    title: "Local by default",
    body: "Your code stays on your machine. No uploads, no telemetry leaks.",
  },
  {
    title: "Versioned JSON output",
    body: "Plug into your own pipeline. Schema is stable and documented.",
  },
];

function Icon({ title }: { title: string }) {
  const paths: Record<string, React.ReactNode> = {
    "Real-time diagnosis": (
      <>
        <circle cx="12" cy="12" r="3" />
        <path d="M12 2v3m0 14v3M4.22 4.22l2.12 2.12m11.32 11.32l2.12 2.12M2 12h3m14 0h3M4.22 19.78l2.12-2.12m11.32-11.32l2.12-2.12" />
      </>
    ),
    "Risk-scored actions": (
      <>
        <path d="M12 2L2 7v6c0 5 4 9 10 10 6-1 10-5 10-10V7l-10-5z" />
        <path d="M9 12l2 2 4-4" />
      </>
    ),
    "Silent when healthy": (
      <>
        <path d="M11 5L6 9H2v6h4l5 4V5z" />
        <path d="M17 9a5 5 0 010 6" opacity="0.35" />
      </>
    ),
    "Append-only evidence": (
      <>
        <path d="M4 4h14l2 2v14a0 0 0 010 0H4V4z" />
        <path d="M8 10h8M8 14h8M8 18h5" />
      </>
    ),
    "Local by default": (
      <>
        <rect x="3" y="4" width="18" height="12" rx="2" />
        <path d="M8 20h8M12 16v4" />
      </>
    ),
    "Versioned JSON output": (
      <>
        <path d="M8 4H6a2 2 0 00-2 2v4a2 2 0 01-2 2 2 2 0 012 2v4a2 2 0 002 2h2" />
        <path d="M16 4h2a2 2 0 012 2v4a2 2 0 002 2 2 2 0 00-2 2v4a2 2 0 01-2 2h-2" />
      </>
    ),
  };
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="text-accent"
      aria-hidden
    >
      {paths[title]}
    </svg>
  );
}

export default function Benefits() {
  return (
    <Section
      eyebrow="What you get"
      title="Not productivity theater."
      subtitle="Observable, measurable outcomes. Each benefit maps to one concrete capability of the product."
    >
      <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4 sm:gap-5">
        {benefits.map((b) => (
          <Card key={b.title} hoverable>
            <div className="inline-flex h-9 w-9 items-center justify-center rounded-[var(--radius)] border border-accent/25 bg-accent/10 mb-4">
              <Icon title={b.title} />
            </div>
            <h3 className="text-[15px] font-semibold text-text mb-1.5">
              {b.title}
            </h3>
            <p className="text-[14px] text-text-2 leading-relaxed">{b.body}</p>
          </Card>
        ))}
      </div>
    </Section>
  );
}
