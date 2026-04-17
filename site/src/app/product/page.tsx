import type { Metadata } from "next";
import Section from "@/components/Section";
import Card from "@/components/Card";
import Pill from "@/components/Pill";
import CTAButton from "@/components/CTAButton";

export const metadata: Metadata = {
  title: "Product",
  description:
    "Every AI coding session, observable. Detection engine, evidence log, and CLI.",
};

const patterns = [
  {
    name: "correction_chain",
    what: "You correct the AI 3+ times in a row on the same file.",
    cost: "2–5× token waste",
  },
  {
    name: "repeated_edits",
    what: "Same file edited 3+ times — the task was not scoped.",
    cost: "3–4× token waste",
  },
  {
    name: "unsolicited_long_output",
    what: "AI generated 200 lines when you asked for a fix.",
    cost: "Bloated context for every future message",
  },
  {
    name: "exploratory_reading",
    what: "AI read 5+ files with no plan.",
    cost: "Fills context with irrelevant content",
  },
  {
    name: "weak_prompt",
    what: "Complex task with zero constraints.",
    cost: "Speculative output, guaranteed rework",
  },
  {
    name: "large_paste",
    what: "Code dumped into chat instead of referenced.",
    cost: "Permanent context bloat",
  },
];

const commands = [
  { cmd: "orbit run", desc: "Live diagnosis on the current session." },
  { cmd: "orbit stats", desc: "Session summary with active/passive/waste breakdown." },
  { cmd: "orbit doctor", desc: "Structured report with findings and evidence ids." },
  { cmd: "orbit analyze", desc: "Post-session analysis of the conversation log." },
  { cmd: "orbit ui", desc: "Local TUI for inspecting evidence entries." },
  { cmd: "orbit quickstart", desc: "Guided install + first-diagnosis walk-through." },
];

const notThis = [
  "Not a code quality tool. It doesn't review your code.",
  "Not a prompt library. It doesn't write prompts for you.",
  "Not a cost calculator. It doesn't count tokens.",
  "Not an automation tool. It never executes commands.",
];

export default function ProductPage() {
  return (
    <>
      <Section
        eyebrow="Product"
        title="Every AI coding session, observable."
        subtitle="Orbit ships three things: a detection engine that reads your session in real time, an append-only evidence log, and a CLI that exposes both."
      >
        <div className="flex flex-wrap gap-2">
          <Pill tone="blue">orbit run</Pill>
          <Pill tone="blue">orbit stats</Pill>
          <Pill tone="blue">orbit doctor --json</Pill>
          <Pill tone="green">Silent when healthy</Pill>
          <Pill>Append-only evidence</Pill>
        </div>
      </Section>

      <Section eyebrow="Detection engine" title="The six patterns Orbit watches for.">
        <div className="overflow-hidden rounded-[var(--radius-lg)] border border-border">
          <div className="grid grid-cols-[1.1fr_2fr_1fr] items-center gap-x-5 px-5 py-3 border-b border-border bg-bg-2/50 font-mono text-[11px] uppercase tracking-[0.16em] text-text-3">
            <div>Pattern</div>
            <div>What it means</div>
            <div>Typical cost</div>
          </div>
          {patterns.map((p) => (
            <div
              key={p.name}
              className="grid grid-cols-[1.1fr_2fr_1fr] items-center gap-x-5 px-5 py-4 border-b last:border-b-0 border-border/50"
            >
              <code className="font-mono text-[13px] text-accent">{p.name}</code>
              <p className="text-[14px] text-text-2">{p.what}</p>
              <p className="text-[13px] text-text font-mono">{p.cost}</p>
            </div>
          ))}
        </div>
      </Section>

      <Section eyebrow="Evidence log" title="Append-only. Versioned. Public schema.">
        <div className="grid md:grid-cols-3 gap-4 sm:gap-5">
          <Card>
            <h3 className="text-[15px] font-semibold text-text mb-2">Append-only</h3>
            <p className="text-[14px] text-text-2 leading-relaxed">
              Every detection, action, and reconciliation is recorded. Entries are
              never overwritten. You can always replay what the system saw.
            </p>
          </Card>
          <Card>
            <h3 className="text-[15px] font-semibold text-text mb-2">Versioned JSON</h3>
            <p className="text-[14px] text-text-2 leading-relaxed">
              Every record carries a <code className="font-mono text-[12.5px] text-accent">schema</code>{" "}
              field like <code className="font-mono text-[12.5px] text-accent">orbit.doctor.v1</code>.
              Integrations don&apos;t break on upgrade.
            </p>
          </Card>
          <Card>
            <h3 className="text-[15px] font-semibold text-text mb-2">HMAC-signed</h3>
            <p className="text-[14px] text-text-2 leading-relaxed">
              Tracking and reconciliation endpoints support HMAC signing. Enable it
              in environments where integrity matters.
            </p>
          </Card>
        </div>
      </Section>

      <Section eyebrow="CLI" title="Six commands. That's the whole surface.">
        <div className="overflow-hidden rounded-[var(--radius-lg)] border border-border bg-[color:var(--color-mono-bg)]">
          {commands.map((c) => (
            <div
              key={c.cmd}
              className="flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-6 px-5 py-4 border-b last:border-b-0 border-border/50"
            >
              <code className="font-mono text-[13px] text-accent min-w-[180px]">
                $ {c.cmd}
              </code>
              <p className="text-[14px] text-text-2">{c.desc}</p>
            </div>
          ))}
        </div>
        <div className="mt-6">
          <CTAButton href="/docs" variant="secondary" className="h-10 px-4 text-[13px]">
            Read the CLI reference →
          </CTAButton>
        </div>
      </Section>

      <Section eyebrow="Scope" title="What Orbit is not.">
        <div className="grid sm:grid-cols-2 gap-3">
          {notThis.map((n) => (
            <div
              key={n}
              className="flex items-start gap-3 rounded-[var(--radius)] border border-border bg-surface/40 px-4 py-3"
            >
              <span className="mt-[5px] inline-flex h-4 w-4 items-center justify-center rounded-full bg-atrisk/15 text-atrisk">
                <svg viewBox="0 0 24 24" width="10" height="10">
                  <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" strokeWidth="2.5" fill="none" strokeLinecap="round" />
                </svg>
              </span>
              <p className="text-[14px] text-text-2">{n}</p>
            </div>
          ))}
        </div>
      </Section>

      <Section>
        <div className="rounded-[var(--radius-lg)] border border-accent/30 bg-accent/5 p-8 sm:p-10 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-5">
          <div>
            <h2 className="text-2xl font-semibold text-text mb-2">
              Install in 30 seconds.
            </h2>
            <p className="text-text-2">See your first diagnosis on your next complex session.</p>
          </div>
          <CTAButton href="/docs/quickstart" variant="primary">
            Install the free skill
          </CTAButton>
        </div>
      </Section>
    </>
  );
}
