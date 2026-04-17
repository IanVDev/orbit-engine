import type { Metadata } from "next";
import Link from "next/link";
import Section from "@/components/Section";
import Card from "@/components/Card";

export const metadata: Metadata = {
  title: "Docs",
  description:
    "Install guide, CLI reference, JSON schema, and self-hosting for Orbit.",
};

const sections = [
  {
    title: "Quickstart",
    href: "/docs/quickstart",
    body: "Install, trigger, and verify in under two minutes. Start here.",
    tag: "Start here",
  },
  {
    title: "CLI reference",
    href: "/docs#cli",
    body: "Every subcommand, every flag. run, stats, doctor, analyze, ui, quickstart.",
  },
  {
    title: "JSON schema",
    href: "/docs#schema",
    body: "Versioned schemas (orbit.doctor.v1, orbit.session.v1). Stable across upgrades.",
  },
  {
    title: "Self-hosting",
    href: "/docs#self-host",
    body: "Run the engine inside your own infrastructure. Docker Compose notes.",
  },
  {
    title: "Evidence methodology",
    href: "/docs/evidence",
    body: "How we measure before/after. Which sessions. Which logs. Reproducible.",
  },
];

export default function DocsPage() {
  return (
    <>
      <Section
        eyebrow="Docs"
        title="Everything you need to run Orbit."
        subtitle="Short by design. If something needs a page, it has one. If it doesn't, you won't find filler here."
      />

      <Section>
        <div className="grid md:grid-cols-2 gap-4 sm:gap-5">
          {sections.map((s) => (
            <Link key={s.title} href={s.href} className="block group">
              <Card hoverable className="h-full">
                <div className="flex items-start justify-between gap-3 mb-2">
                  <h3 className="text-[16px] font-semibold text-text group-hover:text-accent transition-colors">
                    {s.title}
                  </h3>
                  {s.tag && (
                    <span className="inline-flex items-center rounded-full border border-accent/40 bg-accent/10 px-2 py-0.5 font-mono text-[10px] uppercase tracking-[0.14em] text-accent">
                      {s.tag}
                    </span>
                  )}
                </div>
                <p className="text-[14px] text-text-2 leading-relaxed">{s.body}</p>
              </Card>
            </Link>
          ))}
        </div>
      </Section>

      <Section id="cli" eyebrow="CLI reference" title="Six commands.">
        <div className="prose-orbit">
          <h3 id="cli-run"><code>orbit run</code></h3>
          <p>
            Watches the current AI coding session in real time. Emits a DIAGNOSIS
            block whenever a waste pattern crosses the detection threshold.
            Silent when the session is healthy.
          </p>

          <h3 id="cli-stats"><code>orbit stats</code></h3>
          <p>
            Summarizes a session or a window of sessions. Reports active time,
            passive context, waste, and which patterns were detected.
          </p>

          <h3 id="cli-doctor"><code>orbit doctor</code></h3>
          <p>
            Runs a health check on the environment, the detection pipeline, and
            the evidence log. Use <code>--json</code> to emit a versioned
            structured report (<code>orbit.doctor.v1</code>).
          </p>

          <h3 id="cli-analyze"><code>orbit analyze</code></h3>
          <p>
            Post-session analysis. Replays a conversation log through the
            detection engine without requiring a live session.
          </p>

          <h3 id="cli-ui"><code>orbit ui</code></h3>
          <p>
            Terminal UI for inspecting evidence entries, risk timelines, and
            session composition. Read-only by design.
          </p>

          <h3 id="cli-quickstart"><code>orbit quickstart</code></h3>
          <p>
            Guided first-run experience. Installs the skill, verifies
            activation, and walks through a demo diagnosis.
          </p>
        </div>
      </Section>

      <Section id="schema" eyebrow="Schema" title="Versioned JSON output.">
        <div className="prose-orbit">
          <p>
            Every structured emission from Orbit carries a <code>schema</code>{" "}
            field. Breaking changes ship under a new version number;
            backwards-compatible additions do not. Integrations build against
            the version, not a moving target.
          </p>
          <h3>Schemas</h3>
          <ul>
            <li><code>orbit.doctor.v1</code> — diagnostic report</li>
            <li><code>orbit.session.v1</code> — session summary</li>
            <li><code>orbit.event.v1</code> — tracking event</li>
            <li><code>orbit.evidence.v1</code> — append-only evidence entry</li>
          </ul>
        </div>
      </Section>

      <Section id="self-host" eyebrow="Self-hosting" title="Keep everything inside your perimeter.">
        <div className="prose-orbit">
          <p>
            The Orbit engine is a standalone HTTP service that listens on{" "}
            <code>localhost:9100</code> by default. Docker Compose and a minimal
            systemd unit are provided. The engine has no outbound calls unless
            explicitly configured.
          </p>
          <h3>Environment variables</h3>
          <ul>
            <li><code>ORBIT_BACKEND_URL</code> — where the skill sends events</li>
            <li><code>ORBIT_HMAC_SECRET</code> — optional HMAC key for tracking</li>
            <li><code>ORBIT_RECONCILE_SECRET</code> — optional HMAC key for reconcile</li>
          </ul>
        </div>
      </Section>
    </>
  );
}
