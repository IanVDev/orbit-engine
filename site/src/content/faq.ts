export type FaqItem = {
  id: string;
  q: string;
  a: string;
  category: "product" | "privacy" | "integration" | "teams";
};

export const faq: FaqItem[] = [
  {
    id: "what-is-orbit",
    q: "What is Orbit?",
    a: "Orbit is an operational visibility layer for AI coding sessions. It watches how you work with tools like Claude Code, GPT, and Gemini, detects waste patterns in real time, and records every decision in an append-only evidence log.",
    category: "product",
  },
  {
    id: "who-is-it-for",
    q: "Who is Orbit for?",
    a: "Developers, tech leads, and engineering teams who use AI coding assistants daily and want to understand — with evidence — how those sessions actually run. If AI is an occasional tool for you, Orbit is probably overkill.",
    category: "product",
  },
  {
    id: "claude-code",
    q: "Is Orbit tied to Claude Code?",
    a: "No. Orbit works with Claude Code, GPT, Gemini, and any AI coding assistant that exposes a session stream. Claude Code is the first-class integration because of its skill system, but the detection engine is assistant-agnostic.",
    category: "integration",
  },
  {
    id: "stores-prompts",
    q: "Does Orbit store my prompts?",
    a: "No. Orbit analyzes patterns in the session, not the text of your prompts. Nothing you type is stored, exported, or sent to a third party. Default deployment is entirely local.",
    category: "privacy",
  },
  {
    id: "stores-code",
    q: "Does Orbit store my code?",
    a: "No. Orbit never reads file contents beyond what's already in the session. No repository is cloned, no file is uploaded. References are kept as paths, not bodies.",
    category: "privacy",
  },
  {
    id: "cost-vs-value",
    q: "What's the difference between \"cost\" and \"value\" in Orbit's view?",
    a: "Cost is what you pay in tokens. Value is what the session produced. Orbit makes both observable — and shows when the first grows without the second. It doesn't just measure spend, it measures efficiency.",
    category: "product",
  },
  {
    id: "vs-token-dashboard",
    q: "How is this different from a token dashboard?",
    a: "A token dashboard tells you how much you spent. Orbit tells you why — which behaviors in the session caused the cost, and which specific action would have prevented it. It's diagnosis, not accounting.",
    category: "product",
  },
  {
    id: "individual",
    q: "Can I use Orbit as an individual?",
    a: "Yes. The free tier is designed for individual use: full pattern detection, real-time diagnoses, no account, no login. You install it once and it works.",
    category: "product",
  },
  {
    id: "team",
    q: "Can a team use Orbit?",
    a: "Yes. Orbit Pro aggregates patterns across a team's sessions without exposing prompts or code. Leads see patterns and risks at a team level; developers keep full privacy inside their sessions.",
    category: "teams",
  },
  {
    id: "install",
    q: "How do I install it?",
    a: "Drag the skill/ folder (or SKILL.md) into your AI assistant. That takes around 30 seconds. The first diagnosis appears on your next complex session. Full guide in /docs/quickstart.",
    category: "product",
  },
  {
    id: "integration",
    q: "Does Orbit integrate with my existing tools?",
    a: "Yes. Orbit emits versioned JSON that any pipeline can consume. The CLI is designed to pipe into your own dashboards, OpenTelemetry collectors, or internal ingestion systems.",
    category: "integration",
  },
  {
    id: "self-host",
    q: "Can I self-host Orbit?",
    a: "Yes. The engine is a standalone service (HTTP on localhost by default). Docker Compose and deployment notes are in /docs/self-hosting. Your data never leaves your infrastructure.",
    category: "integration",
  },
  {
    id: "time-to-value",
    q: "How long before I see value?",
    a: "One session. The first time Orbit detects a waste pattern during your session, you'll see a diagnosis with the action to take. Most users see that in under 15 minutes of normal use.",
    category: "product",
  },
  {
    id: "secure",
    q: "Is Orbit secure?",
    a: "Yes. Orbit runs locally by default, supports HMAC-signed events on the tracking channel, and exposes an append-only evidence log that cannot be rewritten silently. The public threat model explains every assumption.",
    category: "privacy",
  },
  {
    id: "records",
    q: "What does Orbit actually record?",
    a: "Orbit records: detection events, risk scores, recommended actions, reconciliation entries, and schema-versioned diagnoses. It does not record: your prompts, your code, your file contents, or any personally identifiable session content.",
    category: "privacy",
  },
  {
    id: "differentiator",
    q: "What makes Orbit different?",
    a: "Three things. It diagnoses, it doesn't just count. It records evidence, it doesn't just report. And it treats an AI session as something you can observe, not something you have to guess about after the fact.",
    category: "product",
  },
];
