import type { Metadata } from "next";
import { Account } from "./Account";

export const metadata: Metadata = { title: "Account" };

export default function AccountPage() {
  return <Account />;
}
