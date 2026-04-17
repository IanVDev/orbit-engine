import type { Metadata } from "next";
import Section from "@/components/Section";
import Terminal, { TCmd, TOk, TWarn } from "@/components/Terminal";
import CTAButton from "@/components/CTAButton";

export const metadata: Metadata = {
  title: "Quickstart",
  description: "Install Orbit in 30 seconds and see your first diagnosis.",
};

export default function QuickstartPage() {
  return (
    <>
      <Section
        eyebrow="Quickstart"
        title="From zero to first diagnosis, in two minutes."
        subtitle="Three steps. No account. No integration work. If you can drag a file into Claude Code, you can install Orbit."
      />

      <Section eyebrow="Step 1" title="Install">
        <div className="prose-orbit max-w-3xl">
          <p>
            Drop the <code>skill/</code> folder into your AI coding assistant.
            Claude Code supports drag-and-drop of skill folders directly; other
            assistants use the same file layout.
          </p>
          <p>
            Alternatively, drag <code>SKILL.md</code> by itself — it always
            works as a fallback.
          </p>
        </div>
      </Section>

      <Section eyebrow="Step 2" title="Trigger">
        <div className="prose-orbit max-w-3xl mb-6">
          <p>
            Paste this into your AI assistant to trigger the skill on a complex
            task:
          </p>
        </div>
        <Terminal title="trigger phrase">
{`create a data ingestion service in TypeScript with Kafka, validation and PostgreSQL`}
        </Terminal>
        <div className="prose-orbit max-w-3xl mt-5">
          <p>
            The skill activates automatically on complex tasks. On a fresh
            session with no history, it may not fire — Step 3 covers that.
          </p>
        </div>
      </Section>

      <Section eyebrow="Step 3" title="Verify">
        <div className="prose-orbit max-w-3xl mb-6">
          <p>
            Look at the response. One of two things happened:
          </p>
        </div>

        <h3 className="text-[15px] font-semibold text-healthy mb-3">
          ✓ You see DIAGNOSIS
        </h3>
        <Terminal title="healthy">
          <TWarn>DIAGNOSIS</TWarn>
          {"\n- Complex task started without Plan Mode"}
          {"\n..."}
          {"\n"}
          <TCmd>Risk: high</TCmd>
          {"\n\n"}
          <TOk>ACTIONS</TOk>
          {"\n1. ..."}
        </Terminal>
        <p className="mt-3 text-[14px] text-text-2">
          The skill is active. Follow the actions. You&apos;re done.
        </p>

        <h3 className="text-[15px] font-semibold text-degraded mt-10 mb-3">
          ! You don&apos;t see DIAGNOSIS
        </h3>
        <div className="prose-orbit max-w-3xl mb-5">
          <p>
            The auto-trigger uses heuristics — it may not fire on a fresh
            session with no history. That&apos;s expected. It&apos;s not a sign
            the skill isn&apos;t installed.
          </p>
          <p>Use the <strong>guaranteed prompt</strong> instead:</p>
        </div>
        <Terminal title="guaranteed prompt">
{`Before answering, apply orbit-engine. Then: create a data ingestion service in TypeScript with Kafka, validation and PostgreSQL`}
        </Terminal>
        <div className="prose-orbit max-w-3xl mt-5">
          <p>This phrase always activates the skill, regardless of session state.</p>
          <ul>
            <li><strong>DIAGNOSIS appeared</strong> → skill is active. Auto-trigger just needed context. You&apos;re good.</li>
            <li><strong>Still no DIAGNOSIS</strong> → the skill is not loaded. Go back to Step 1 and reinstall.</li>
          </ul>
        </div>
      </Section>

      <Section>
        <div className="rounded-[var(--radius-lg)] border border-accent/30 bg-accent/5 p-8 sm:p-10 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-5">
          <div>
            <h2 className="text-2xl font-semibold text-text mb-2">
              Next: read the CLI reference.
            </h2>
            <p className="text-text-2">Six commands. That&apos;s the whole surface.</p>
          </div>
          <CTAButton href="/docs" variant="primary">
            Read the docs
          </CTAButton>
        </div>
      </Section>
    </>
  );
}
