import { cn } from "@/lib/cn";

type Tone = "default" | "green" | "amber" | "red" | "blue";

const toneStyles: Record<Tone, string> = {
  default: "border-border text-text-2",
  green: "border-healthy/35 text-healthy",
  amber: "border-degraded/35 text-degraded",
  red: "border-atrisk/35 text-atrisk",
  blue: "border-accent/35 text-accent",
};

export default function Pill({
  tone = "default",
  children,
  className,
}: {
  tone?: Tone;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border bg-bg-2/50 px-2.5 py-1 font-mono text-[11px] leading-none",
        toneStyles[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
