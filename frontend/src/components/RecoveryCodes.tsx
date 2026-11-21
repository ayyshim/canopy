"use client";

import { CopyButton } from "@/components/CopyButton";
import { recoveryCodesText } from "@/lib/twoFactor";

function download(codes: string[]) {
  const url = URL.createObjectURL(new Blob([recoveryCodesText(codes)], { type: "text/plain" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = "canopy-recovery-codes.txt";
  a.click();
  URL.revokeObjectURL(url);
}

/** The recovery codes with Copy and Download. */
export function RecoveryCodes({ codes }: { codes: string[] }) {
  return (
    <>
      <ul className="recovery-codes mono" aria-label="Recovery codes">
        {codes.map((c) => <li key={c}>{c}</li>)}
      </ul>
      <div className="row">
        <CopyButton text={codes.join("\n")} label="Copy codes" />
        <button type="button" className="btn btn-sm" onClick={() => download(codes)}>Download</button>
      </div>
    </>
  );
}
