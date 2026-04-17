import { cn } from "@/lib/cn";

export default function Terminal({
  title,
  children,
  className,
  elevated = false,
}: {
  title?: string;
  children: React.ReactNode;
  className?: string;
  elevated?: boolean;
}) {
  return (
    <div
      className={cn(
        "rounded-[var(--radius-lg)] border border-border overflow-hidden bg-[color:var(--color-mono-bg)]",
        elevated &&
          "shadow-[0_20px_80px_-30px_rgba(0,0,0,0.8),0_0_0_1px_rgba(124,156,255,0.04)]",
        className,
      )}
    >
      <div className="flex items-center gap-1.5 border-b border-border bg-bg-2/60 px-3 py-2">
        <span className="h-2.5 w-2.5 rounded-full bg-[#3a3f4a]" />
        <span className="h-2.5 w-2.5 rounded-full bg-[#3a3f4a]" />
        <span className="h-2.5 w-2.5 rounded-full bg-[#3a3f4a]" />
        {title && (
          <span className="ml-3 font-mono text-[10.5px] text-text-3 tracking-wide">
            {title}
          </span>
        )}
      </div>
      <pre className="font-mono text-[12px] leading-[1.65] text-text p-4 sm:p-5 m-0 whitespace-pre-wrap break-words no-scrollbar overflow-auto">
        {children}
      </pre>
    </div>
  );
}

/* Inline helpers for highlighting inside Terminal children */
export const TCmd = ({ children }: { children: React.ReactNode }) => (
  <span className="text-accent">{children}</span>
);
export const TOk = ({ children }: { children: React.ReactNode }) => (
  <span className="text-healthy">{children}</span>
);
export const TWarn = ({ children }: { children: React.ReactNode }) => (
  <span className="text-degraded">{children}</span>
);
export const TErr = ({ children }: { children: React.ReactNode }) => (
  <span className="text-atrisk">{children}</span>
);
export const TDim = ({ children }: { children: React.ReactNode }) => (
  <span className="text-text-3">{children}</span>
);
export const TKey = ({ children }: { children: React.ReactNode }) => (
  <span className="text-[#c3a6ff]">{children}</span>
);
export const TStr = ({ children }: { children: React.ReactNode }) => (
  <span className="text-[#9acd9a]">{children}</span>
);
