package auth

import (
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA1 secret "12345678901234567890", six digits.
func TestTotpMatchesRFC6238(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		if got := TotpCode(secret, time.Unix(unix, 0)); got != want {
			t.Errorf("t=%d: got %s, want %s", unix, got, want)
		}
	}
}

func TestCheckTotpAllowsOneStepOfDrift(t *testing.T) {
	secret := NewTotpSecret()
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		offset time.Duration
		ok     bool
	}{{0, true}, {-30 * time.Second, true}, {30 * time.Second, true}, {-90 * time.Second, false}, {90 * time.Second, false}} {
		_, ok := CheckTotp(secret, TotpCode(secret, now.Add(tc.offset)), now)
		if ok != tc.ok {
			t.Errorf("offset %v: ok=%v, want %v", tc.offset, ok, tc.ok)
		}
	}
	if _, ok := CheckTotp(secret, "12345", now); ok {
		t.Error("short code accepted")
	}
}

func TestRecoveryCodeHashIgnoresFormatting(t *testing.T) {
	codes := NewRecoveryCodes(RecoveryCodeCount)
	if len(codes) != 10 || codes[0] == codes[1] {
		t.Fatalf("codes: %v", codes)
	}
	a := string(HashRecoveryCode(codes[0]))
	if string(HashRecoveryCode(" "+codes[0]+" ")) != a || string(HashRecoveryCode("ABCDE-FGHIJ")) != string(HashRecoveryCode("abcdefghij")) {
		t.Error("hash depends on case, dashes or spaces")
	}
}
