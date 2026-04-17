import { cn } from "@/lib/cn";

export default function Card({
  children,
  className,
  as = "div",
  hoverable = false,
}: {
  children: React.ReactNode;
  className?: string;
  as?: "div" | "article" | "li";
  hoverable?: boolean;
}) {
  const Tag = as;
  return (
    <Tag
      className={cn(
        "rounded-[var(--radius-lg)] border border-border bg-surface/70 p-5 sm:p-6",
        "transition-colors duration-200",
        hoverable && "hover:border-border/80 hover:bg-surface",
        className,
      )}
    >
      {children}
    </Tag>
  );
}
