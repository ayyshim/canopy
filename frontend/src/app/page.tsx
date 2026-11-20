import { Suspense } from "react";
import { HomeRedirect } from "./HomeRedirect";

export default function Home() {
  return (
    <Suspense>
      <HomeRedirect />
    </Suspense>
  );
}
