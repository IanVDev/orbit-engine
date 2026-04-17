import Link from "next/link";
import { cn } from "@/lib/cn";

type Variant = "primary" | "secondary" | "ghost";

const variantStyles: Record<Variant, string> = {
  primary:
    "bg-accent text-[#0a0c14] hover:bg-[#93afff] border border-accent shadow-[0_6px_24px_-10px_rgba(124,156,255,0.6)]",
  secondary:
    "bg-surface text-text border border-border hover:border-border/80 hover:bg-surface-2",
  ghost:
    "bg-transparent text-text-2 border border-transparent hover:text-text hover:border-border",
};

export default function CTAButton({
  href,
  variant = "primary",
  className,
  children,
  onClick,
  target,
  rel,
}: {
  href?: string;
  variant?: Variant;
  className?: string;
  children: React.ReactNode;
  onClick?: () => void;
  target?: string;
  rel?: string;
}) {
  const classes = cn(
    "inline-flex items-center justify-center gap-2 rounded-[var(--radius)] px-5 h-11 text-sm font-medium",
    "transition-all duration-200",
    variantStyles[variant],
    className,
  );

  if (href) {
    return (
      <Link href={href} className={classes} target={target} rel={rel}>
        {children}
      </Link>
    );
  }
  return (
    <button className={classes} onClick={onClick}>
      {children}
    </button>
  );
}
