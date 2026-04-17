import Section from "@/components/Section";

const use = [
  "You use AI coding assistants daily.",
  "You care about why a session went long, not just how long.",
  "You want evidence, not anecdotes.",
];

const skip = [
  "You open an AI assistant once a week for a quick question.",
  "You're looking for a chat UI or a prompt library.",
  "You need a code review tool (we don't review code).",
];

function Mark({ ok }: { ok: boolean }) {
  return (
    <span
      className={
        "mt-[5px] inline-flex h-4 w-4 flex-none items-center justify-center rounded-full " +
        (ok ? "bg-healthy/15 text-healthy" : "bg-atrisk/15 text-atrisk")
      }
    >
      <svg viewBox="0 0 24 24" width="10" height="10" aria-hidden>
        {ok ? (
          <path
            d="M5 12l4 4L19 7"
            stroke="currentColor"
            strokeWidth="2.5"
            fill="none"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        ) : (
          <path
            d="M6 6l12 12M18 6L6 18"
            stroke="currentColor"
            strokeWidth="2.5"
            fill="none"
            strokeLinecap="round"
          />
        )}
      </svg>
    </span>
  );
}

export default function IsSkip() {
  return (
    <Section eyebrow="Qualification" title="Is Orbit for you?">
      <div className="grid md:grid-cols-2 gap-4 sm:gap-5">
        <div className="rounded-[var(--radius-lg)] border border-healthy/25 bg-healthy/[0.04] p-6">
          <h3 className="text-[15px] font-semibold text-text mb-4">Use Orbit if…</h3>
          <ul className="space-y-3">
            {use.map((u) => (
              <li key={u} className="flex items-start gap-3 text-[14px] text-text-2 leading-relaxed">
                <Mark ok />
                <span>{u}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="rounded-[var(--radius-lg)] border border-border bg-surface/40 p-6">
          <h3 className="text-[15px] font-semibold text-text mb-4">Skip Orbit if…</h3>
          <ul className="space-y-3">
            {skip.map((s) => (
              <li key={s} className="flex items-start gap-3 text-[14px] text-text-2 leading-relaxed">
                <Mark ok={false} />
                <span>{s}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </Section>
  );
}
