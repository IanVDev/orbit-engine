import Link from "next/link";
import Logo from "./Logo";
import CTAButton from "./CTAButton";

const nav = [
  { href: "/product", label: "Product" },
  { href: "/docs", label: "Docs" },
  { href: "/pricing", label: "Pricing" },
  { href: "/security", label: "Security" },
  { href: "/changelog", label: "Changelog" },
];

export default function Header() {
  return (
    <header className="sticky top-0 z-40 w-full border-b border-border/60 bg-bg/80 backdrop-blur-xl supports-[backdrop-filter]:bg-bg/60">
      <div className="mx-auto flex h-16 w-full max-w-[var(--container-site)] items-center justify-between px-5 sm:px-8">
        <div className="flex items-center gap-10">
          <Logo />
          <nav className="hidden md:flex items-center gap-7">
            {nav.map((item) => (
              <Link
                key={item.href}
                href={item.href}
                className="text-[13px] text-text-2 transition-colors hover:text-text"
              >
                {item.label}
              </Link>
            ))}
          </nav>
        </div>
        <div className="flex items-center gap-2">
          <Link
            href="/docs/quickstart"
            className="hidden sm:inline-flex h-9 items-center px-3 text-[13px] text-text-2 hover:text-text"
          >
            Quickstart
          </Link>
          <CTAButton
            href="/docs/quickstart"
            variant="primary"
            className="h-9 px-4 text-[13px]"
          >
            Install free
          </CTAButton>
        </div>
      </div>
    </header>
  );
}
