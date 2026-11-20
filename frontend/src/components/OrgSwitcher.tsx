"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, currentOrg, ROLE_LABEL, type Me } from "@/lib/api";
import { useMe } from "@/lib/me";
import { LogoMark } from "./LogoMark";
import { Menu } from "./Menu";

/** Organization switcher: each organization with your role, plus Create organization. */
export function OrgSwitcher() {
  const { me, setMe } = useMe();
  const router = useRouter();
  const org = currentOrg(me);

  async function switchTo(id: string) {
    setMe(await api<Me>("PATCH", "/me", { last_org_id: id }));
    router.push("/board");
  }

  return (
    <Menu
      ariaLabel="Switch organization"
      label={
        <>
          {org && <LogoMark name={org.name} url={org.logo_url} size={20} />}
          <span className="wsn">{org?.name ?? "No organization"}</span>
          <span aria-hidden="true">▾</span>
        </>
      }
    >
      {(close) => (
        <>
          <div className="ml">Organizations</div>
          {me.organizations.map((o) => (
            <button
              key={o.id}
              role="menuitem"
              className="mi"
              aria-current={o.id === me.current_org_id}
              onClick={() => {
                close();
                if (o.id !== me.current_org_id) void switchTo(o.id);
              }}
            >
              <LogoMark name={o.name} url={o.logo_url} size={20} />
              <span className="spacer">{o.name}</span>
              <span className="pill">{ROLE_LABEL[o.role]}</span>
            </button>
          ))}
          <hr />
          <Link role="menuitem" className="mi" href="/new-org" onClick={close}>
            Create organization
          </Link>
        </>
      )}
    </Menu>
  );
}
