import type { Metadata } from "next";
import Section from "@/components/Section";
import { faq } from "@/content/faq";

export const metadata: Metadata = {
  title: "FAQ",
  description: "Frequently asked questions about Orbit.",
};

const categories = [
  { id: "product", label: "Product" },
  { id: "privacy", label: "Privacy" },
  { id: "integration", label: "Integration" },
  { id: "teams", label: "Teams" },
] as const;

export default function FaqPage() {
  return (
    <>
      <Section
        eyebrow="FAQ"
        title="Every answer, in plain language."
        subtitle="Each answer opens with a complete, self-sufficient sentence. Skim, or read end to end."
      />
      {categories.map((cat) => {
        const items = faq.filter((f) => f.category === cat.id);
        if (items.length === 0) return null;
        return (
          <Section key={cat.id} eyebrow={cat.label}>
            <div className="rounded-[var(--radius-lg)] border border-border bg-surface/40 divide-y divide-border/60 overflow-hidden">
              {items.map((item) => (
                <details key={item.id} className="group">
                  <summary className="flex cursor-pointer list-none items-center justify-between gap-5 px-5 py-4 text-[15px] text-text [&::-webkit-details-marker]:hidden">
                    <span>{item.q}</span>
                    <span className="inline-flex h-6 w-6 flex-none items-center justify-center rounded-full border border-border text-text-3 transition-transform group-open:rotate-45 group-open:border-accent/50 group-open:text-accent">
                      <svg width="12" height="12" viewBox="0 0 24 24" aria-hidden>
                        <path d="M12 5v14M5 12h14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
                      </svg>
                    </span>
                  </summary>
                  <div className="px-5 pb-5 pt-0 text-[14px] text-text-2 leading-relaxed max-w-3xl">
                    {item.a}
                  </div>
                </details>
              ))}
            </div>
          </Section>
        );
      })}
    </>
  );
}
