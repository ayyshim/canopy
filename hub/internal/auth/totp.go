package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 authenticator apps use HMAC-SHA1
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpPeriod = 30
	totpDigits = 6
	// TotpSkew is how many steps either side of now still count, for clock drift.
	totpSkew = 1
	// RecoveryCodeCount is how many single-use codes enrollment hands out.
	RecoveryCodeCount = 10
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTotpSecret returns a random 160-bit secret, base32 encoded.
func NewTotpSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

// TotpURL is the otpauth URL that authenticator apps read from the QR code.
func TotpURL(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"},
		"digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

func totpAt(secret string, step int64) (string, bool) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", false
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1_000_000), true
}

// TotpCode is the code an authenticator app shows at t.
func TotpCode(secret string, t time.Time) string {
	c, _ := totpAt(secret, totpStep(t))
	return c
}

// CheckTotp reports the time step code matches, within one step of now.
// The caller must reject a step it has already accepted.
func CheckTotp(secret, code string, now time.Time) (step int64, ok bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	cur := totpStep(now)
	for s := cur - totpSkew; s <= cur+totpSkew; s++ {
		want, valid := totpAt(secret, s)
		if valid && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}

// NewRecoveryCodes returns n codes shaped like abcde-fghij.
func NewRecoveryCodes(n int) []string {
	out := make([]string, n)
	for i := range out {
		b := make([]byte, 6)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		s := strings.ToLower(b32.EncodeToString(b))[:10]
		out[i] = s[:5] + "-" + s[5:]
	}
	return out
}

// HashRecoveryCode hashes a code the way tokens are hashed, ignoring case, dashes and spaces.
func HashRecoveryCode(code string) []byte {
	r := strings.NewReplacer("-", "", " ", "")
	return HashToken(strings.ToLower(r.Replace(strings.TrimSpace(code))))
}
