import { cn } from "@/lib/cn";
import Container from "./Container";

export default function Section({
  id,
  eyebrow,
  title,
  subtitle,
  className,
  containerClassName,
  children,
}: {
  id?: string;
  eyebrow?: string;
  title?: React.ReactNode;
  subtitle?: React.ReactNode;
  className?: string;
  containerClassName?: string;
  children?: React.ReactNode;
}) {
  return (
    <section
      id={id}
      className={cn(
        "py-16 sm:py-24 border-t border-border/60 first:border-t-0",
        className,
      )}
    >
      <Container className={containerClassName}>
        {(eyebrow || title || subtitle) && (
          <header className="mb-10 sm:mb-14 max-w-3xl">
            {eyebrow && (
              <div className="font-mono text-[11px] uppercase tracking-[0.18em] text-accent mb-4">
                {eyebrow}
              </div>
            )}
            {title && (
              <h2 className="text-3xl sm:text-4xl font-semibold tracking-tight text-text leading-[1.1]">
                {title}
              </h2>
            )}
            {subtitle && (
              <p className="mt-4 text-lg text-text-2 leading-relaxed">
                {subtitle}
              </p>
            )}
          </header>
        )}
        {children}
      </Container>
    </section>
  );
}
