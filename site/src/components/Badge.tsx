import { cn } from "@/lib/cn";

export default function Badge({
  children,
  pulse = false,
  className,
}: {
  children: React.ReactNode;
  pulse?: boolean;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-2 rounded-full border border-border bg-bg-2/70 px-3 py-1",
        "font-mono text-[10.5px] uppercase tracking-[0.14em] text-text-2",
        className,
      )}
    >
      {pulse && (
        <span className="relative flex h-1.5 w-1.5">
          <span className="absolute inline-flex h-full w-full rounded-full bg-healthy opacity-75 animate-ping" />
          <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-healthy" />
        </span>
      )}
      {children}
    </span>
  );
}
