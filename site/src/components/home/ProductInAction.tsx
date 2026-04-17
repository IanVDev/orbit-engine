"use client";

import { useState } from "react";
import Section from "@/components/Section";
import Terminal, {
  TCmd,
  TDim,
  TErr,
  TKey,
  TOk,
  TStr,
  TWarn,
} from "@/components/Terminal";
import CTAButton from "@/components/CTAButton";
import { cn } from "@/lib/cn";

const tabs = [
  { id: "run", label: "orbit run", desc: "Live diagnosis" },
  { id: "stats", label: "orbit stats", desc: "Session summary" },
  { id: "doctor", label: "orbit doctor --json", desc: "Structured report" },
] as const;

type TabId = (typeof tabs)[number]["id"];

export default function ProductInAction() {
  const [tab, setTab] = useState<TabId>("run");

  return (
    <Section
      id="product-in-action"
      eyebrow="Product in action"
      title="Orbit, running."
      subtitle="Real output from real sessions. The CLI is how everything surfaces — human-readable in the terminal, machine-readable in JSON."
    >
      <div className="flex flex-wrap gap-2 mb-5">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={cn(
              "group inline-flex items-center gap-2 rounded-[var(--radius)] border px-3.5 py-2 text-[12.5px] transition-colors",
              tab === t.id
                ? "bg-accent/10 border-accent/40 text-text"
                : "bg-surface border-border text-text-2 hover:text-text hover:border-border/80",
            )}
          >
            <code className="font-mono text-[12px]">{t.label}</code>
            <span className="text-text-3 text-[11.5px]">— {t.desc}</span>
          </button>
        ))}
      </div>

      {tab === "run" && (
        <Terminal title="session-042 · orbit run" elevated>
          <TCmd>$ orbit run</TCmd>
          {"\n"}
          <TDim>{"▸ session: session-042\n▸ watching conversation stream..."}</TDim>
          {"\n\n"}
          <TWarn>DIAGNOSIS</TWarn>
          {"\n- 3 correction cycles on src/ingest.ts"}
          {"\n- AI generated 412 lines; scope required ~90"}
          {"\n- Plan Mode not used on complex task"}
          {"\n\n"}
          <TErr>Risk: high</TErr>
          {"\n\n"}
          <TOk>ACTIONS</TOk>
          {"\n1. Stop current thread. Re-scope with explicit constraints."}
          {"\n2. Clear context. Restart with Plan Mode."}
          {"\n3. Reference file by path, do not paste."}
          {"\n\n"}
          <TDim>Recorded · evidence id:</TDim> <TStr>ev_7f2a91</TStr>
        </Terminal>
      )}

      {tab === "stats" && (
        <Terminal title="orbit stats --session session-042" elevated>
          <TCmd>$ orbit stats --session session-042</TCmd>
          {"\n"}Session: session-042        Duration: 47m
          {"\n"}Messages: 31                Turns: 14
          {"\n\n"}
          <TOk>Activity</TOk>
          {"\n  active input:  9m  20%"}
          {"\n  passive ctx:  33m  70%"}
          {"\n  waste:         5m  10%"}
          {"\n\n"}
          <TWarn>Patterns detected</TWarn>
          {"\n  correction_chain    x2  risk: high"}
          {"\n  repeated_edits      x1  risk: medium"}
          {"\n  weak_prompt         x1  risk: medium"}
          {"\n\n"}
          <TDim>Evidence entries: 17</TDim>
        </Terminal>
      )}

      {tab === "doctor" && (
        <Terminal title="orbit doctor --json" elevated>
          <TCmd>$ orbit doctor --json</TCmd>
          {"\n{"}
          {"\n  "}
          <TKey>&quot;schema&quot;</TKey>: <TStr>&quot;orbit.doctor.v1&quot;</TStr>,
          {"\n  "}
          <TKey>&quot;status&quot;</TKey>: <TStr>&quot;degraded&quot;</TStr>,
          {"\n  "}
          <TKey>&quot;findings&quot;</TKey>: [
          {"\n    {"}
          {"\n      "}
          <TKey>&quot;name&quot;</TKey>: <TStr>&quot;correction_chain&quot;</TStr>,
          {"\n      "}
          <TKey>&quot;detail&quot;</TKey>: <TStr>&quot;3 consecutive corrections on src/ingest.ts&quot;</TStr>,
          {"\n      "}
          <TKey>&quot;risk&quot;</TKey>: <TStr>&quot;high&quot;</TStr>,
          {"\n      "}
          <TKey>&quot;evidence_id&quot;</TKey>: <TStr>&quot;ev_7f2a91&quot;</TStr>
          {"\n    }"}
          {"\n  ],"}
          {"\n  "}
          <TKey>&quot;recorded_at&quot;</TKey>: <TStr>&quot;2026-04-17T05:50:00Z&quot;</TStr>
          {"\n}"}
        </Terminal>
      )}

      <div className="mt-6">
        <CTAButton href="/docs" variant="secondary" className="h-10 px-4 text-[13px]">
          Explore the CLI reference →
        </CTAButton>
      </div>
    </Section>
  );
}
