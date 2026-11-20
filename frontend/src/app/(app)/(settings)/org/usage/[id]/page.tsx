import { PersonUsage } from "./PersonUsage";

export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function PersonUsagePage() {
  return <PersonUsage />;
}
