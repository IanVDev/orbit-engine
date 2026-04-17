import Hero from "@/components/home/Hero";
import ProofStrip from "@/components/home/ProofStrip";
import Problem from "@/components/home/Problem";
import HowItWorks from "@/components/home/HowItWorks";
import Benefits from "@/components/home/Benefits";
import ProductInAction from "@/components/home/ProductInAction";
import BeforeAfter from "@/components/home/BeforeAfter";
import UseCases from "@/components/home/UseCases";
import IsSkip from "@/components/home/IsSkip";
import Traceability from "@/components/home/Traceability";
import HomeFaq from "@/components/home/HomeFaq";
import FinalCta from "@/components/home/FinalCta";

export default function HomePage() {
  return (
    <>
      <Hero />
      <ProofStrip />
      <Problem />
      <HowItWorks />
      <Benefits />
      <ProductInAction />
      <BeforeAfter />
      <UseCases />
      <IsSkip />
      <Traceability />
      <HomeFaq />
      <FinalCta />
    </>
  );
}
