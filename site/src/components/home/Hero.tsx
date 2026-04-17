import Badge from "@/components/Badge";
import CTAButton from "@/components/CTAButton";
import Container from "@/components/Container";
import Terminal, { TCmd, TDim, TErr, TOk, TStr, TWarn } from "@/components/Terminal";

export default function Hero() {
  return (
    <section className="relative overflow-hidden">
      <div className="absolute inset-0 bg-grid opacity-70 pointer-events-none" />
      <div className="absolute inset-0 bg-gradient-to-b from-transparent via-transparent to-bg pointer-events-none" />

      <Container className="relative pt-20 pb-24 sm:pt-28 sm:pb-32">
        <div className="grid lg:grid-cols-[1.1fr_1fr] gap-12 lg:gap-14 items-center">
          <div className="animate-fadeup">
            <Badge pulse>Now in Public Beta · v1.0</Badge>

            <h1 className="mt-6 text-[2.75rem] sm:text-6xl font-semibold tracking-[-0.022em] leading-[1.02] text-text">
              Operational visibility
              <br />
              for{" "}
              <span className="relative inline-block">
                <span className="text-accent">AI coding sessions.</span>
                <span className="absolute -bottom-1 left-0 h-px w-full bg-gradient-to-r from-accent/60 via-accent/20 to-transparent" />
              </span>
            </h1>

            <p className="mt-6 max-w-xl text-[17px] sm:text-lg text-text-2 leading-[1.55]">
              Orbit watches how you work with{" "}
              <span className="text-text">Claude Code</span>,{" "}
              <span className="text-text">GPT</span>, and{" "}
              <span className="text-text">Gemini</span>
              &nbsp;— detects waste patterns, records every decision, and shows
              you exactly what changed.{" "}
              <span className="text-text">Evidence, not estimates.</span>
            </p>

            <div className="mt-8 flex flex-col sm:flex-row gap-3 sm:items-center">
              <CTAButton href="/docs/quickstart" variant="primary">
                Install the free skill
              </CTAButton>
              <CTAButton href="#product-in-action" variant="ghost">
                See it in action ↓
              </CTAButton>
            </div>

            <p className="mt-6 font-mono text-[11.5px] text-text-3 tracking-wide">
              Runs locally · No prompts stored · No code uploaded
            </p>
          </div>

          <div className="relative animate-fadeup" style={{ animationDelay: "120ms" }}>
            <div className="absolute -inset-2 rounded-[20px] bg-gradient-to-br from-accent/15 via-transparent to-transparent blur-2xl pointer-events-none" />
            <Terminal title="session-042 · orbit run" elevated className="relative">
{`` }<TCmd>$ orbit run</TCmd>
{`\n`}<TDim>▸ session: session-042{`\n`}▸ watching conversation stream...</TDim>
{`\n\n`}<TWarn>DIAGNOSIS</TWarn>
{`\n- 3 correction cycles on src/ingest.ts`}
{`\n- AI generated 412 lines; scope required ~90`}
{`\n- Plan Mode not used on complex task`}
{`\n\n`}<TErr>Risk: high</TErr>
{`\n\n`}<TOk>ACTIONS</TOk>
{`\n1. Stop current thread. Re-scope with explicit constraints.`}
{`\n2. Clear context. Restart with Plan Mode.`}
{`\n3. Reference file by path, do not paste.`}
{`\n\n`}<TDim>Recorded · evidence id:</TDim> <TStr>ev_7f2a91</TStr>
            </Terminal>
          </div>
        </div>
      </Container>
    </section>
  );
}
