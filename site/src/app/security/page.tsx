import type { Metadata } from "next";
import Section from "@/components/Section";
import Card from "@/components/Card";
import Pill from "@/components/Pill";

export const metadata: Metadata = {
  title: "Security & Privacy",
  description:
    "Orbit runs locally by default. No prompts stored. No code uploaded. Public threat model.",
};

const guarantees = [
  {
    title: "Your code stays on your machine",
    body: "Orbit never reads file contents beyond what's already in the session. No repo is cloned. No file body is uploaded. References are kept as paths.",
  },
  {
    title: "Your prompts are not stored",
    body: "Orbit analyzes patterns in the session, not the text of your prompts. Nothing you type is persisted, exported, or sent to a third party.",
  },
  {
    title: "Append-only evidence log",
    body: "Every record carries an evidence id. Entries are never overwritten. You can always replay what the system saw and acted on.",
  },
  {
    title: "Local by default",
    body: "The engine listens on localhost:9100 by default. No outbound calls unless explicitly configured. Self-hosting is a first-class deployment.",
  },
];

export default function SecurityPage() {
  return (
    <>
      <Section
        eyebrow="Security & Privacy"
        title="Observability without compromise."
        subtitle="Privacy is an architectural property of Orbit, not a marketing claim. This page explains exactly what happens to every byte."
      >
        <div className="flex flex-wrap gap-2">
          <Pill tone="blue">Local by default</Pill>
          <Pill tone="green">No prompts stored</Pill>
          <Pill tone="green">No code uploaded</Pill>
          <Pill tone="blue">HMAC-signed events</Pill>
          <Pill tone="blue">Public threat model</Pill>
        </div>
      </Section>

      <Section id="privacy" eyebrow="Privacy" title="What Orbit actually handles.">
        <div className="grid md:grid-cols-2 gap-4 sm:gap-5">
          {guarantees.map((g) => (
            <Card key={g.title}>
              <h3 className="text-[16px] font-semibold text-text mb-2">{g.title}</h3>
              <p className="text-[14px] text-text-2 leading-relaxed">{g.body}</p>
            </Card>
          ))}
        </div>
      </Section>

      <Section eyebrow="What Orbit records" title="The exact list. Nothing more.">
        <div className="grid md:grid-cols-2 gap-4">
          <Card className="border-healthy/20 bg-healthy/[0.03]">
            <h3 className="text-[15px] font-semibold text-text mb-3">Recorded</h3>
            <ul className="space-y-2 text-[14px] text-text-2">
              <li>— Detection events (pattern id, risk score)</li>
              <li>— Recommended actions</li>
              <li>— Reconciliation entries (estimated vs actual)</li>
              <li>— Schema-versioned diagnoses</li>
              <li>— Evidence ids and timestamps</li>
            </ul>
          </Card>
          <Card className="border-atrisk/20 bg-atrisk/[0.03]">
            <h3 className="text-[15px] font-semibold text-text mb-3">Not recorded</h3>
            <ul className="space-y-2 text-[14px] text-text-2">
              <li>— Prompt text</li>
              <li>— Source code bodies</li>
              <li>— File contents</li>
              <li>— Personally identifiable session content</li>
              <li>— Anything outside the session stream</li>
            </ul>
          </Card>
        </div>
      </Section>

      <Section id="threat-model" eyebrow="Threat model" title="Assumptions, stated publicly.">
        <div className="prose-orbit max-w-3xl">
          <p>
            The Orbit threat model is versioned and public. It covers: deployment
            postures (local, self-hosted, SaaS), trust boundaries, signing
            assumptions, log tampering resistance, and the specific failure
            modes of each detection pattern.
          </p>
          <h3>Signed channels</h3>
          <p>
            The tracking and reconciliation endpoints support HMAC signing via{" "}
            <code>ORBIT_HMAC_SECRET</code> and <code>ORBIT_RECONCILE_SECRET</code>.
            In any deployment crossing trust boundaries, signing is expected.
          </p>
          <h3>Air-gapped operation</h3>
          <p>
            Orbit can run fully offline. No feature requires network egress. Set{" "}
            <code>ORBIT_BACKEND_URL</code> to a private address and omit all
            public telemetry.
          </p>
          <h3>Responsible disclosure</h3>
          <p>
            Security issues: email <code>security@orbit.dev</code>. We
            acknowledge within 48 hours and publish postmortems in the changelog.
          </p>
        </div>
      </Section>
    </>
  );
}
