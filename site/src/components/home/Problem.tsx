import Section from "@/components/Section";
import Card from "@/components/Card";

const items = [
  {
    title: "Invisible rework",
    body: "You don't notice you corrected the AI four times on the same file.",
  },
  {
    title: "Opaque cost",
    body: "Token bills grow without explanation. Nobody knows which session burned the budget.",
  },
  {
    title: "Anecdotal decisions",
    body: "\u201CAI feels slower this week\u201D is not a signal. It's a feeling.",
  },
];

export default function Problem() {
  return (
    <Section
      eyebrow="The problem"
      title="You can't improve what you can't see."
      subtitle="Most AI coding sessions are invisible. The waste is mechanical — and fixable — if you can observe it."
    >
      <div className="grid md:grid-cols-3 gap-4 sm:gap-5">
        {items.map((item) => (
          <Card key={item.title} className="flex flex-col gap-2">
            <h3 className="text-[15px] font-semibold text-text">{item.title}</h3>
            <p className="text-[14px] text-text-2 leading-relaxed">{item.body}</p>
          </Card>
        ))}
      </div>
    </Section>
  );
}
