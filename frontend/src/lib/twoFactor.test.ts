import test from "node:test";
import assert from "node:assert/strict";
import { cleanCode, cleanRecoveryCode, isCodeComplete, lowOnRecoveryCodes, pendingStep, recoveryCodesText, recoveryHint, unauthorizedPath, safeNext, twoFactorPath } from "./twoFactor.ts";

test("cleanCode keeps digits and stops at six", () => {
  assert.equal(cleanCode("123 456"), "123456");
  assert.equal(cleanCode("12a3-4"), "1234");
  assert.equal(cleanCode("1234567890"), "123456");
  assert.equal(cleanCode(""), "");
});

test("isCodeComplete needs six digits", () => {
  assert.equal(isCodeComplete("12345"), false);
  assert.equal(isCodeComplete("123456"), true);
});

test("cleanRecoveryCode trims and lowercases", () => {
  assert.equal(cleanRecoveryCode("  ABCD-1234 \n"), "abcd-1234");
});

test("twoFactorPath sends each step to its page", () => {
  assert.equal(twoFactorPath("enroll"), "/two-factor/setup");
  assert.equal(twoFactorPath("verify"), "/two-factor");
  assert.equal(twoFactorPath("enroll", "/setup"), "/two-factor/setup?next=%2Fsetup");
});

test("safeNext refuses other sites", () => {
  assert.equal(safeNext("/admin"), "/admin");
  assert.equal(safeNext("//evil.com"), undefined);
  assert.equal(safeNext("https://evil.com"), undefined);
  assert.equal(safeNext(null), undefined);
});

test("recoveryCodesText lists every code", () => {
  const t = recoveryCodesText(["a", "b"]);
  assert.match(t, /\na\nb\n/);
});

test("pendingStep reads the two_factor answer", () => {
  assert.equal(pendingStep({ two_factor: "enroll" }), "enroll");
  assert.equal(pendingStep({ two_factor: "verify" }), "verify");
  assert.equal(pendingStep({ user: {} }), undefined);
});

test("unauthorizedPath follows the two-factor 401", () => {
  assert.equal(unauthorizedPath({ code: "two_factor_required", next: "enroll" }), "/two-factor/setup");
  assert.equal(unauthorizedPath({ code: "two_factor_required", next: "verify" }), "/two-factor");
  assert.equal(unauthorizedPath({ code: "unauthorized" }), "/login");
});

test("lowOnRecoveryCodes flags two or fewer", () => {
  assert.equal(lowOnRecoveryCodes(3), false);
  assert.equal(lowOnRecoveryCodes(2), true);
  assert.equal(lowOnRecoveryCodes(0), true);
});

test("recoveryHint uses plain words", () => {
  assert.match(recoveryHint(1), /1 recovery code left/);
  assert.match(recoveryHint(2), /2 recovery codes left/);
  assert.match(recoveryHint(0), /no recovery codes left/);
});
