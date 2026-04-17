import Link from "next/link";
import { cn } from "@/lib/cn";

export default function Logo({ className }: { className?: string }) {
  return (
    <Link
      href="/"
      className={cn(
        "group inline-flex items-center gap-2.5 text-text font-mono text-[13px] tracking-[0.18em]",
        className,
      )}
      aria-label="Orbit home"
    >
      <span className="relative inline-flex h-2.5 w-2.5">
        <span className="absolute inset-0 rounded-full bg-accent opacity-60 blur-[3px] group-hover:opacity-90" />
        <span className="relative inline-block h-2.5 w-2.5 rounded-full bg-accent" />
      </span>
      <span className="font-semibold">ORBIT</span>
    </Link>
  );
}
