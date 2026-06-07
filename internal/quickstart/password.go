package quickstart

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// PasswordCharset is the 3270-typeable unambiguous alphanumeric set used for
// one-time passwords: uppercase letters excluding I, L, O; digits excluding 0
// and 1. Eliminates common transcription errors on physical 3270 keyboards.
const PasswordCharset = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

// GenPassword returns a random password in XXXX-XXXX-XXXX form using
// passwordCharset (12 characters grouped for readability).
func GenPassword() (string, error) {
	b := make([]byte, 12)
	n := big.NewInt(int64(len(PasswordCharset)))
	for i := range b {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		b[i] = PasswordCharset[idx.Int64()]
	}
	return fmt.Sprintf("%s-%s-%s", string(b[0:4]), string(b[4:8]), string(b[8:12])), nil
}
