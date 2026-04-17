import Link from "next/link";
import Logo from "./Logo";

const columns = [
  {
    title: "Product",
    items: [
      { href: "/", label: "Home" },
      { href: "/product", label: "Product" },
      { href: "/pricing", label: "Pricing" },
      { href: "/changelog", label: "Changelog" },
    ],
  },
  {
    title: "Resources",
    items: [
      { href: "/docs", label: "Docs" },
      { href: "/docs/quickstart", label: "Quickstart" },
      { href: "/faq", label: "FAQ" },
      { href: "/docs/evidence", label: "Evidence methodology" },
    ],
  },
  {
    title: "Trust",
    items: [
      { href: "/security", label: "Security" },
      { href: "/security#privacy", label: "Privacy" },
      { href: "/security#threat-model", label: "Threat model" },
      { href: "/changelog", label: "Status" },
    ],
  },
  {
    title: "Company",
    items: [
      { href: "/contact", label: "Contact" },
      { href: "https://github.com/", label: "GitHub" },
    ],
  },
];

export default function Footer() {
  return (
    <footer className="border-t border-border/60 bg-bg-2/30">
      <div className="mx-auto w-full max-w-[var(--container-site)] px-5 sm:px-8 py-12 sm:py-16">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-8 mb-10">
          {columns.map((col) => (
            <div key={col.title}>
              <h4 className="font-mono text-[10.5px] uppercase tracking-[0.18em] text-text-3 mb-4">
                {col.title}
              </h4>
              <ul className="space-y-2.5">
                {col.items.map((item) => (
                  <li key={item.href}>
                    <Link
                      href={item.href}
                      className="text-[13px] text-text-2 hover:text-text transition-colors"
                    >
                      {item.label}
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
        <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 pt-8 border-t border-border/50">
          <Logo />
          <p className="font-mono text-[11px] text-text-3">
            © 2026 Orbit · Built for operators who ship.
          </p>
        </div>
      </div>
    </footer>
  );
}
