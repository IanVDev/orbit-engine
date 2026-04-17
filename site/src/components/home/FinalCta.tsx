import CTAButton from "@/components/CTAButton";
import Container from "@/components/Container";

export default function FinalCta() {
  return (
    <section className="relative overflow-hidden border-t border-border/60">
      <div className="absolute inset-0 bg-grid opacity-40 pointer-events-none" />
      <div className="absolute inset-0 bg-gradient-to-br from-accent/8 via-transparent to-transparent pointer-events-none" />

      <Container className="relative py-20 sm:py-28 text-center">
        <h2 className="text-3xl sm:text-5xl font-semibold tracking-tight text-text leading-[1.05] max-w-3xl mx-auto">
          Start measuring your{" "}
          <span className="text-accent">AI coding workflow.</span>
        </h2>
        <p className="mt-5 text-[17px] text-text-2 max-w-2xl mx-auto leading-relaxed">
          Install in 30 seconds. See your first diagnosis in your next session.
        </p>
        <div className="mt-8 flex flex-col sm:flex-row gap-3 items-center justify-center">
          <CTAButton href="/docs/quickstart" variant="primary">
            Install the free skill
          </CTAButton>
          <CTAButton href="/contact" variant="secondary">
            Talk to us about teams
          </CTAButton>
        </div>
        <p className="mt-6 font-mono text-[11.5px] text-text-3">
          Free forever for individuals · Pro for teams
        </p>
      </Container>
    </section>
  );
}
