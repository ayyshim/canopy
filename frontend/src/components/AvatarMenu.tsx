"use client";

import Link from "next/link";
import { currentOrg, initials, isOrgAdmin } from "@/lib/api";
import { useMe } from "@/lib/me";
import { Menu } from "./Menu";
import { logOut } from "./LogoutButton";

export function Avatar({ name, color, size = 28 }: { name: string; color: string; size?: number }) {
  return (
    <span className="avatar" style={{ background: color, width: size, height: size }} aria-hidden="true">
      {initials(name)}
    </span>
  );
}

export function AvatarMenu() {
  const { me } = useMe();
  const admin = isOrgAdmin(currentOrg(me)?.role);
  return (
    <Menu ariaLabel="Your account" align="right" buttonClass="btn btn-ghost" label={<Avatar name={me.user.name} color={me.user.avatar_color} />}>
      {(close) => (
        <>
          <div className="ml">
            {me.user.name}
            <br />
            {me.user.email}
          </div>
          <Link role="menuitem" className="mi" href="/account" onClick={close}>Account</Link>
          {admin && (
            <Link role="menuitem" className="mi" href="/org/general" onClick={close}>Organization settings</Link>
          )}
          {me.user.is_hub_admin && (
            <Link role="menuitem" className="mi" href="/admin" onClick={close}>Hub admin</Link>
          )}
          <hr />
          <button role="menuitem" className="mi" onClick={() => void logOut()}>Log out</button>
        </>
      )}
    </Menu>
  );
}
