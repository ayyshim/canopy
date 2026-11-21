import type { components } from "./api-types";

export type TwoFactorPending = components["schemas"]["SignInResult"];
export type TwoFactorStep = components["schemas"]["SignInResult"]["two_factor"];
export type TwoFactorSetup = components["schemas"]["TwoFactorSetup"];
export type TwoFactorEnabled = { recovery_codes: string[]; me: components["schemas"]["Me"] };

export const CODE_LENGTH = 6;

/** Keeps digits only, so pasted codes like "123 456" work. Capped at six digits. */
export function cleanCode(raw: string): string {
  return raw.replace(/\D/g, "").slice(0, CODE_LENGTH);
}

export function isCodeComplete(code: string): boolean {
  return code.length === CODE_LENGTH;
}

/** Recovery codes are typed by hand: trim and ignore case. */
export function cleanRecoveryCode(raw: string): string {
  return raw.trim().toLowerCase();
}

export function twoFactorPath(step: TwoFactorStep, next?: string): string {
  const base = step === "enroll" ? "/two-factor/setup" : "/two-factor";
  const safe = safeNext(next);
  return safe ? `${base}?next=${encodeURIComponent(safe)}` : base;
}

/** Only same-site paths are allowed as a destination after sign-in. */
export function safeNext(next: string | null | undefined): string | undefined {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return undefined;
  return next;
}

/** The text file people download with their recovery codes. */
export function recoveryCodesText(codes: string[]): string {
  return ["Canopy recovery codes", "Each code works once. Keep them somewhere safe.", "", ...codes, ""].join("\n");
}

/** A sign-in answer is either the person (done) or the next two-factor step. */
export function pendingStep(res: object): TwoFactorStep | undefined {
  const step = (res as { two_factor?: unknown }).two_factor;
  return step === "enroll" || step === "verify" ? step : undefined;
}

/** Where a 401 from the hub should send someone: a two-factor page, or sign in. */
export function unauthorizedPath(err: { code: string; next?: string }): string {
  if (err.code === "two_factor_required" && (err.next === "enroll" || err.next === "verify")) return twoFactorPath(err.next);
  return "/login";
}

export const LOW_RECOVERY_CODES = 2;

export function lowOnRecoveryCodes(left: number): boolean {
  return left <= LOW_RECOVERY_CODES;
}

/** The hint shown when few recovery codes remain. */
export function recoveryHint(left: number): string {
  if (left <= 0) return "You have no recovery codes left. Get new ones so you can sign in if you lose your phone.";
  return `You have ${left} recovery ${left === 1 ? "code" : "codes"} left. Get new ones so you can sign in if you lose your phone.`;
}
