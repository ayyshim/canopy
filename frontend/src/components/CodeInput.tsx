"use client";

import { cleanCode } from "@/lib/twoFactor";

type Props = {
  id: string;
  value: string;
  onChange: (code: string) => void;
  invalid?: boolean;
  describedBy?: string;
  autoFocus?: boolean;
};

/** A six-digit code field: digits only, one-time-code autofill, and pasted codes are cleaned. */
export function CodeInput({ id, value, onChange, invalid, describedBy, autoFocus }: Props) {
  return (
    <input
      id={id}
      className="input code-input mono"
      type="text"
      inputMode="numeric"
      autoComplete="one-time-code"
      pattern="[0-9]*"
      spellCheck={false}
      autoCapitalize="off"
      autoFocus={autoFocus}
      placeholder="000000"
      aria-invalid={invalid}
      aria-describedby={describedBy}
      value={value}
      onChange={(e) => onChange(cleanCode(e.target.value))}
      onPaste={(e) => {
        e.preventDefault();
        onChange(cleanCode(e.clipboardData.getData("text")));
      }}
    />
  );
}
