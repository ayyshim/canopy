"use client";

import { useRef, useState } from "react";
import { attachFiles, removeAttachment } from "@/lib/board/actions";
import { isAdmin, personName, visibleMember } from "@/lib/board/derive";
import type { AttachmentView } from "@/lib/types";
import { useB, useBoardCtx } from "../context";

export const MAX_FILE_MB = 25;

export function fileSize(n: number) {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1024) return `${Math.round(n / 1024)} KB`;
  return `${n} B`;
}

const fileUrl = (a: AttachmentView, download = false) => `/api/v1/attachments/${a.id}/file${download ? "?download=1" : ""}`;

/** Attachments lists the files on a goal or task, with a button to add more. */
export function Attachments({ goalId, taskId, canAdd = true }: { goalId?: string; taskId?: string; canAdd?: boolean }) {
  const b = useB();
  const { me } = useBoardCtx();
  const [busy, setBusy] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const list = (b.attachments ?? []).filter((a) => (goalId && a.goal_id === goalId) || (taskId && a.task_id === taskId));
  const who = (a: AttachmentView) => (a.member_id ? visibleMember(b, a.member_id)?.name ?? "An agent" : personName(b, a.user_id));

  const add = async (files: FileList | null) => {
    if (!files?.length) return;
    setBusy(true);
    await attachFiles(goalId ? { goalId } : { taskId: taskId! }, [...files]);
    setBusy(false);
    if (input.current) input.current.value = "";
  };

  return (
    <div className="sb-sec">
      <span className="lbl">Attachments</span>
      {list.length ? (
        <ul className="files">
          {list.map((a) => (
            <li key={a.id}>
              <a href={fileUrl(a)} target="_blank" rel="noreferrer" className="linkbtn" title={a.name}>{a.name}</a>
              <span className="hint">{fileSize(a.size)} · {who(a)}</span>
              <span className="spacer" />
              <a href={fileUrl(a, true)} className="linkbtn" aria-label={`Download ${a.name}`}>Download</a>
              {(a.user_id === me || isAdmin(b)) && <button className="linkbtn" onClick={() => removeAttachment(a.id)} aria-label={`Remove ${a.name}`}>Remove</button>}
            </li>
          ))}
        </ul>
      ) : <p className="hint">No files yet. Agents can download attached files and share their own.</p>}
      {canAdd && (
        <>
          <input ref={input} type="file" multiple hidden onChange={(e) => add(e.target.files)} />
          <button className="btn btn-sm" style={{ alignSelf: "flex-start" }} disabled={busy} onClick={() => input.current?.click()}>{busy ? "Uploading…" : "Attach files"}</button>
        </>
      )}
    </div>
  );
}

/** FilePicker holds files chosen in a form until the goal or task exists. */
export function FilePicker({ files, onChange }: { files: File[]; onChange: (f: File[]) => void }) {
  const input = useRef<HTMLInputElement>(null);
  const tooBig = files.filter((f) => f.size > MAX_FILE_MB << 20);
  return (
    <div className="field"><span>Attachments <span style={{ fontWeight: 500 }}>(optional)</span></span>
      {files.length > 0 && (
        <ul className="files">
          {files.map((f, i) => (
            <li key={f.name + i}>
              <span>{f.name}</span><span className="hint">{fileSize(f.size)}</span><span className="spacer" />
              <button type="button" className="linkbtn" onClick={() => onChange(files.filter((_, j) => j !== i))} aria-label={`Remove ${f.name}`}>Remove</button>
            </li>
          ))}
        </ul>
      )}
      <input ref={input} type="file" multiple hidden onChange={(e) => { onChange([...files, ...(e.target.files ?? [])]); e.target.value = ""; }} />
      <button type="button" className="btn btn-sm" style={{ alignSelf: "flex-start" }} onClick={() => input.current?.click()}>Add files</button>
      <span className="hint">Images, documents or data, up to {MAX_FILE_MB} MB each. Agents can open them.</span>
      {tooBig.length > 0 && <span className="err">{tooBig.map((f) => f.name).join(", ")} {tooBig.length > 1 ? "are" : "is"} over {MAX_FILE_MB} MB.</span>}
    </div>
  );
}
