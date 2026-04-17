"use client";

import { useState } from "react";
import Section from "@/components/Section";
import CTAButton from "@/components/CTAButton";
import { faq } from "@/content/faq";
import { cn } from "@/lib/cn";

const featuredIds = [
  "what-is-orbit",
  "vs-token-dashboard",
  "stores-prompts",
  "stores-code",
  "claude-code",
  "install",
];

export default function HomeFaq() {
  const [open, setOpen] = useState<string | null>(featuredIds[0]);
  const items = featuredIds
    .map((id) => faq.find((f) => f.id === id))
    .filter(Boolean) as typeof faq;

  return (
    <Section
      eyebrow="FAQ"
      title="Six answers before the obvious questions."
      subtitle="The rest live on the full FAQ page. Nothing here is legalese — every answer opens with a complete, self-sufficient sentence."
    >
      <div className="rounded-[var(--radius-lg)] border border-border bg-surface/40 divide-y divide-border/60 overflow-hidden">
        {items.map((item) => {
          const isOpen = open === item.id;
          return (
            <details
              key={item.id}
              open={isOpen}
              onToggle={(e) =>
                setOpen((e.currentTarget as HTMLDetailsElement).open ? item.id : null)
              }
              className="group"
            >
              <summary
                className={cn(
                  "flex cursor-pointer list-none items-center justify-between gap-5 px-5 py-4",
                  "text-[15px] text-text hover:text-text transition-colors",
                  "[&::-webkit-details-marker]:hidden",
                )}
              >
                <span>{item.q}</span>
                <span
                  className={cn(
                    "inline-flex h-6 w-6 flex-none items-center justify-center rounded-full border border-border text-text-3 transition-transform",
                    isOpen && "rotate-45 text-accent border-accent/50",
                  )}
                >
                  <svg width="12" height="12" viewBox="0 0 24 24" aria-hidden>
                    <path d="M12 5v14M5 12h14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
                  </svg>
                </span>
              </summary>
              <div className="px-5 pb-5 pt-0 text-[14px] text-text-2 leading-relaxed max-w-3xl">
                {item.a}
              </div>
            </details>
          );
        })}
      </div>

      <div className="mt-6">
        <CTAButton href="/faq" variant="secondary" className="h-10 px-4 text-[13px]">
          See all questions →
        </CTAButton>
      </div>
    </Section>
  );
}
