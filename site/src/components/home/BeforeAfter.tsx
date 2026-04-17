import Section from "@/components/Section";

type Row = {
  label: string;
  before: string;
  after: string;
  delta: string;
  ratio: number; // 0..1 (how much of the "before" was reduced)
};

const rows: Row[] = [
  { label: "Lines generated", before: "812", after: "169", delta: "−79%", ratio: 0.79 },
  { label: "Tokens consumed", before: "~6,059", after: "~1,051", delta: "−83%", ratio: 0.83 },
  { label: "Rework cycles", before: "3", after: "0", delta: "−100%", ratio: 1 },
  { label: "Unnecessary files", before: "6", after: "0", delta: "−6", ratio: 1 },
];

export default function BeforeAfter() {
  return (
    <Section
      eyebrow="Before vs after"
      title="Same task. Measured twice."
      subtitle="A single data ingestion service. Same requirements. Before Orbit, and after."
    >
      <div className="rounded-[var(--radius-lg)] border border-border bg-surface/40 overflow-hidden">
        <div className="grid grid-cols-[1.4fr_repeat(3,_1fr)] items-center gap-x-4 px-5 py-3 border-b border-border bg-bg-2/50 text-[11px] uppercase tracking-[0.16em] text-text-3 font-mono">
          <div>Metric</div>
          <div>Without Orbit</div>
          <div>With Orbit</div>
          <div className="text-right">Δ</div>
        </div>
        {rows.map((row) => (
          <div
            key={row.label}
            className="grid grid-cols-[1.4fr_repeat(3,_1fr)] items-center gap-x-4 px-5 py-5 border-b last:border-b-0 border-border/50"
          >
            <div className="text-[14px] text-text">{row.label}</div>
            <div className="font-mono text-[14px] text-text-2">{row.before}</div>
            <div className="font-mono text-[14px] text-text">{row.after}</div>
            <div className="flex justify-end items-center gap-3">
              <div className="hidden sm:block w-24 h-1.5 rounded-full bg-border/50 overflow-hidden">
                <div
                  className="h-full bg-gradient-to-r from-accent to-healthy"
                  style={{ width: `${Math.round(row.ratio * 100)}%` }}
                />
              </div>
              <span className="font-mono text-[13px] font-medium text-healthy">
                {row.delta}
              </span>
            </div>
          </div>
        ))}
      </div>
      <p className="mt-4 text-[12.5px] font-mono text-text-3">
        Methodology and raw session logs available in{" "}
        <a href="/docs/evidence" className="text-accent hover:underline">
          /docs/evidence
        </a>
        .
      </p>
    </Section>
  );
}
