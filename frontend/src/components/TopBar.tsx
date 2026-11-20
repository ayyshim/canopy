"use client";

import { AvatarMenu } from "./AvatarMenu";
import { Logo } from "./Logo";
import { LogoutButton } from "./LogoutButton";
import { OrgSwitcher } from "./OrgSwitcher";

/** Board top bar (SPEC "The board"); workspace controls arrive with workspaces. */
export function TopBar({ children }: { children?: React.ReactNode }) {
  return (
    <header className="topbar">
      <Logo href="/board" />
      <span className="sep" aria-hidden="true" />
      <OrgSwitcher />
      {children}
      <span className="spacer" />
      <AvatarMenu />
      <LogoutButton />
    </header>
  );
}
