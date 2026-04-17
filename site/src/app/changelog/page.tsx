import type { Metadata } from "next";
import Section from "@/components/Section";
import Pill from "@/components/Pill";

export const metadata: Metadata = {
  title: "Changelog",
  description: "Public changelog for the Orbit engine and skill.",
};

type Entry = {
  date: string;
  version: string;
  type: "feat" | "fix" | "docs" | "security";
  title: string;
  body: string;
};

const entries: Entry[] = [
  {
    date: "Apr 17, 2026",
    version: "v1.0.0",
    type: "feat",
    title: "Public Beta launch",
    body:
      "First publicly available version of Orbit. Full detection engine with six waste patterns, append-only evidence log, CLI with run / stats / doctor / analyze / ui / quickstart, and versioned JSON output (orbit.doctor.v1, orbit.session.v1).",
  },
  {
    date: "Apr 16, 2026",
    version: "v0.9.6",
    type: "feat",
    title: "Strict UX audit across all CLI commands",
    body:
      "Added G5 strict audit test to verify every orbit subcommand adheres to the standard UX output patterns. Ensures consistent risk labels, action blocks, and evidence id footers.",
  },
  {
    date: "Apr 16, 2026",
    version: "v0.9.5",
    type: "feat",
    title: "Atomic JSON report emission",
    body:
      "orbit doctor --json now buffers output before writing, then commits atomically. No more partial JSON on interruption. Includes contract tests for writeJSONAtomic and PrintJSON.",
  },
  {
    date: "Apr 15, 2026",
    version: "v0.9.4",
    type: "feat",
    title: "Formal doctor JSON schema",
    body:
      "Doctor output is now versioned under orbit.doctor.v1 with separate Name / Detail fields and a fail-closed error envelope. Integrations can rely on the shape across upgrades.",
  },
  {
    date: "Apr 14, 2026",
    version: "v0.9.3",
    type: "feat",
    title: "--json flag on orbit doctor",
    body:
      "Structured diagnostic reporting available via orbit doctor --json. Backwards-compatible text output unchanged.",
  },
];

const typeTone: Record<Entry["type"], "blue" | "green" | "default" | "amber"> = {
  feat: "blue",
  fix: "green",
  docs: "default",
  security: "amber",
};

export default function ChangelogPage() {
  return (
    <>
      <Section
        eyebrow="Changelog"
        title="Every release, on the record."
        subtitle="Dates are absolute. Versions are real. No marketing entries."
      />
      <Section>
        <div className="space-y-8">
          {entries.map((e) => (
            <article
              key={e.version + e.title}
              className="grid md:grid-cols-[200px_1fr] gap-5 sm:gap-8 pb-8 border-b border-border/50 last:border-b-0"
            >
              <div>
                <div className="font-mono text-[12px] text-text-3 mb-1">
                  {e.date}
                </div>
                <div className="font-mono text-[13px] text-text font-semibold">
                  {e.version}
                </div>
              </div>
              <div>
                <div className="mb-2">
                  <Pill tone={typeTone[e.type]}>{e.type}</Pill>
                </div>
                <h3 className="text-[17px] font-semibold text-text mb-2">
                  {e.title}
                </h3>
                <p className="text-[14px] text-text-2 leading-relaxed max-w-3xl">
                  {e.body}
                </p>
              </div>
            </article>
          ))}
        </div>
      </Section>
    </>
  );
}
