import Container from "@/components/Container";

const items = [
  "Claude Code",
  "GPT / ChatGPT",
  "Gemini",
  "Any CLI-based AI assistant",
];

export default function ProofStrip() {
  return (
    <section className="border-y border-border/50 bg-bg-2/30">
      <Container className="py-10">
        <p className="text-center font-mono text-[11px] uppercase tracking-[0.18em] text-text-3 mb-6">
          Built for developers who ship with AI daily · Works with the tools you already use
        </p>
        <div className="flex flex-wrap items-center justify-center gap-x-10 gap-y-4">
          {items.map((item) => (
            <span
              key={item}
              className="text-[13px] font-medium text-text-2/80 tracking-wide"
            >
              {item}
            </span>
          ))}
        </div>
      </Container>
    </section>
  );
}
