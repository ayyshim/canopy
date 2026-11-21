"use client";

import { useId, useState } from "react";
import { CodeInput } from "@/components/CodeInput";
import { Modal } from "@/components/Modal";
import { RecoveryCodes } from "@/components/RecoveryCodes";
import { Section } from "@/components/PageShell";
import { api, ApiError, formatDate, hardNavigate } from "@/lib/api";
import { useMe } from "@/lib/me";
import { isCodeComplete, lowOnRecoveryCodes, recoveryHint } from "@/lib/twoFactor";

type Action = "codes" | "change";

/** Asks for an authenticator code, then runs the action. */
function CodeStep({ title, intro, button, onClose, run }: {
  title: string;
  intro: string;
  button: string;
  onClose: () => void;
  run: (code: string) => Promise<void>;
}) {
  const id = useId();
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(ev: React.FormEvent) {
    ev.preventDefault();
    if (!isCodeComplete(code)) return setError("Enter the 6-digit code from your authenticator app.");
    setBusy(true);
    setError("");
    try {
      await run(code);
    } catch (err) {
      setBusy(false);
      setCode("");
      setError(err instanceof ApiError ? err.message : "Something went wrong. Try again.");
    }
  }

  return (
    <Modal title={title} onClose={onClose}>
      <form className="stack" onSubmit={submit} noValidate>
        <p className="lead">{intro}</p>
        {error && <div className="banner bad" role="alert" id={id + "-e"}>{error}</div>}
        <div className="field">
          <label htmlFor={id}>6-digit code</label>
          <CodeInput id={id} value={code} onChange={setCode} invalid={Boolean(error)} describedBy={error ? id + "-e" : undefined} />
        </div>
        <div className="row">
          <span className="spacer" />
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn btn-primary" disabled={busy}>{busy ? "Checking…" : button}</button>
        </div>
      </form>
    </Modal>
  );
}

/** Account settings: two-step sign-in status, new recovery codes and changing the app. */
export function TwoStepSection() {
  const { me, refresh } = useMe();
  const status = me.two_factor;
  const [action, setAction] = useState<Action | null>(null);
  const [codes, setCodes] = useState<string[] | null>(null);
  const [saved, setSaved] = useState(false);
  const left = status.recovery_codes_left;

  return (
    <Section title="Two-step sign-in" sub="You enter a code from an authenticator app after your password.">
      <div className="row"><span className="spacer">Status</span><span className="pill ok">On{status.enabled_at ? ` since ${formatDate(status.enabled_at)}` : ""}</span></div>
      <div className="row"><span className="spacer">Recovery codes left</span><span>{left}</span></div>
      {lowOnRecoveryCodes(left) && <div className="banner warn" role="status">{recoveryHint(left)}</div>}
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setAction("codes")}>Get new recovery codes</button>
        <button type="button" className="btn" onClick={() => setAction("change")}>Change authenticator app</button>
      </div>
      {action === "codes" && (
        <CodeStep
          title="Get new recovery codes"
          intro="Your old recovery codes stop working. Enter a code from your authenticator app to continue."
          button="Get new codes"
          onClose={() => setAction(null)}
          run={async (code) => {
            const r = await api<{ recovery_codes: string[] }>("POST", "/me/two-factor/recovery-codes", { code });
            setCodes(r.recovery_codes);
            setSaved(false);
            setAction(null);
          }}
        />
      )}
      {action === "change" && (
        <CodeStep
          title="Change authenticator app"
          intro="You’ll be signed out everywhere else and set up a new app next. Enter a code from your current app to continue."
          button="Continue"
          onClose={() => setAction(null)}
          run={async (code) => {
            await api("POST", "/me/two-factor/change", { code });
            hardNavigate("/two-factor/setup?next=" + encodeURIComponent("/account"));
          }}
        />
      )}
      {codes && (
        <Modal title="Save your new recovery codes" onClose={() => saved && setCodes(null)}>
          <p className="lead">Each code works once. We show them only now, so keep them somewhere safe.</p>
          <RecoveryCodes codes={codes} />
          <label className="check-row">
            <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
            <span>I saved these codes</span>
          </label>
          <div className="row">
            <span className="spacer" />
            <button type="button" className="btn btn-primary" disabled={!saved} onClick={() => { setCodes(null); void refresh(); }}>Done</button>
          </div>
        </Modal>
      )}
    </Section>
  );
}
