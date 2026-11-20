"use client";

import { useId } from "react";

type Props = {
  label: string;
  error?: string;
  hint?: React.ReactNode;
  children: (props: { id: string; "aria-invalid": boolean; "aria-describedby"?: string }) => React.ReactNode;
};

/** A labelled form field with an inline error under it. */
export function Field({ label, error, hint, children }: Props) {
  const id = useId();
  const describedBy = [hint ? id + "-hint" : "", error ? id + "-error" : ""].filter(Boolean).join(" ") || undefined;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {children({ id, "aria-invalid": Boolean(error), "aria-describedby": describedBy })}
      {hint && <span id={id + "-hint"} className="hint">{hint}</span>}
      {error && <span id={id + "-error"} className="err">{error}</span>}
    </div>
  );
}

/** Moves focus to the first invalid field in a form (after errors render). */
export function focusFirstInvalid(form: HTMLElement | null) {
  requestAnimationFrame(() => {
    form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  });
}
