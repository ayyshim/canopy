"use client";

import { useRef, useState } from "react";
import { api, ApiError, uploadFile } from "@/lib/api";
import { LogoMark } from "./LogoMark";

const TYPES = "image/png,image/jpeg,image/webp,image/svg+xml";
const MAX = 1024 * 1024;

/** Pick, preview and remove a logo. `base` is "/orgs/{id}" or "/workspaces/{id}". */
export function LogoUpload({ base, name, url, onChange }: { base: string; name: string; url?: string | null; onChange: () => unknown }) {
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  async function run(fn: () => Promise<unknown>) {
    setBusy(true);
    setErr("");
    try {
      await fn();
      await onChange();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Something went wrong.");
    } finally {
      setBusy(false);
    }
  }

  function pick(file?: File) {
    if (input.current) input.current.value = "";
    if (!file) return;
    if (file.size > MAX) return setErr("That image is too big. Use one under 1 MB.");
    void run(() => uploadFile(base + "/logo", file));
  }

  return (
    <div className="field">
      <span>Logo</span>
      <div className="row">
        <LogoMark name={name} url={url} size={48} />
        <input ref={input} type="file" accept={TYPES} hidden onChange={(e) => pick(e.target.files?.[0])} />
        <button type="button" className="btn btn-sm" disabled={busy} onClick={() => input.current?.click()}>{url ? "Change" : "Upload"}</button>
        {url && <button type="button" className="btn btn-sm btn-ghost" disabled={busy} onClick={() => run(() => api("DELETE", base + "/logo"))}>Remove</button>}
      </div>
      <span className="hint">PNG, JPG, WebP or SVG, up to 1 MB.</span>
      {err && <span className="err" role="alert">{err}</span>}
    </div>
  );
}
