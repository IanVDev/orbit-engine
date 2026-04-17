import Section from "@/components/Section";
import Card from "@/components/Card";

const cases = [
  {
    role: "Solo developer",
    body: "Understand where your session time actually goes.",
    detail:
      "See active time, passive context, and waste broken down per session. No team dashboard required.",
  },
  {
    role: "Team lead",
    body: "See team patterns without reading every session.",
    detail:
      "Aggregated patterns, never raw prompts or code. Coach on signal, not speculation.",
  },
  {
    role: "Agency / consultancy",
    body: "Justify AI-assisted hours with auditable logs.",
    detail:
      "Evidence ids tie every decision to a timestamped record you can show clients.",
  },
  {
    role: "Platform / DevEx",
    body: "Feed Orbit's JSON into your own internal dashboards.",
    detail:
      "Versioned schema (orbit.*.v1). Pipe through OpenTelemetry, Kafka, or your ingestion of choice.",
  },
];

export default function UseCases() {
  return (
    <Section
      eyebrow="Use cases"
      title="Built for the way you actually ship."
      subtitle="Pick the scenario closest to yours. Orbit adapts to scale, not to role theater."
    >
      <div className="grid md:grid-cols-2 gap-4 sm:gap-5">
        {cases.map((c) => (
          <Card key={c.role} hoverable className="group">
            <div className="font-mono text-[11px] uppercase tracking-[0.16em] text-accent mb-3">
              {c.role}
            </div>
            <p className="text-[16px] font-semibold text-text mb-2 leading-snug">
              {c.body}
            </p>
            <p className="text-[14px] text-text-2 leading-relaxed">{c.detail}</p>
          </Card>
        ))}
      </div>
    </Section>
  );
}
