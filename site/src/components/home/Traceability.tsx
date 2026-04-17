import Section from "@/components/Section";
import Pill from "@/components/Pill";
import CTAButton from "@/components/CTAButton";

const ids = [
  "ev_7f2a91",
  "ev_7f2b04",
  "ev_7f2b22",
  "ev_7f2b57",
  "ev_7f2c01",
  "ev_7f2c48",
];

export default function Traceability() {
  return (
    <Section
      eyebrow="Traceability & trust"
      title="Every signal is recorded. Nothing is hidden."
      subtitle="Orbit is built around an append-only evidence log. Every detection, every action, every reconciliation — stored with a verifiable id."
    >
      <div className="flex flex-wrap gap-2 mb-8">
        <Pill tone="blue">Append-only evidence log</Pill>
        <Pill tone="blue">Versioned JSON schema</Pill>
        <Pill tone="blue">HMAC-signed events (optional)</Pill>
        <Pill tone="blue">Public threat model</Pill>
      </div>

      <div className="rounded-[var(--radius-lg)] border border-border bg-[color:var(--color-mono-bg)] p-5 mb-8 overflow-x-auto no-scrollbar">
        <div className="flex items-center gap-3 font-mono text-[12.5px] text-text-2 whitespace-nowrap">
          {ids.map((id, i) => (
            <span key={id} className="inline-flex items-center gap-3">
              <span className="text-healthy">●</span>
              <span className="text-text">{id}</span>
              {i < ids.length - 1 && <span className="text-text-3">·</span>}
            </span>
          ))}
          <span className="text-text-3">· …</span>
        </div>
      </div>

      <CTAButton href="/security" variant="secondary" className="h-10 px-4 text-[13px]">
        Read the security model →
      </CTAButton>
    </Section>
  );
}
