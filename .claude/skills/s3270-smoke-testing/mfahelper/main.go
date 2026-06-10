// Command mfahelper is a test-only helper for the MFA s3270 smoke script. It
// lives under .claude/ (ignored by `go build ./...`) and is run with `go run`.
// It manipulates MFA state directly in the store and computes live TOTP codes,
// so the smoke script can drive the enrollment/verification screens
// deterministically without an authenticator app.
//
// Usage:
//
//	mfahelper setrequired <db> <user>                  # set mfa_required (pending enrollment)
//	mfahelper enroll <db> <user> <b64key> <b32secret>  # enroll with a known secret
//	mfahelper totp <b32secret>                         # print the current 6-digit code
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/coffeemuse/tn3270proxy/internal/mfa"
	"github.com/coffeemuse/tn3270proxy/internal/store"
	"github.com/pquerna/otp/totp"
)

func main() {
	if len(os.Args) < 2 {
		die("usage: mfahelper <setrequired|enroll|totp> ...")
	}
	ctx := context.Background()
	switch os.Args[1] {
	case "setrequired":
		if len(os.Args) != 4 {
			die("usage: mfahelper setrequired <db> <user>")
		}
		st := open(os.Args[2])
		defer st.Close()
		u, err := st.GetUserByUsername(ctx, os.Args[3])
		check(err)
		check(st.SetMFARequired(ctx, u.ID, true))
	case "enroll":
		if len(os.Args) != 6 {
			die("usage: mfahelper enroll <db> <user> <b64key> <b32secret>")
		}
		st := open(os.Args[2])
		defer st.Close()
		u, err := st.GetUserByUsername(ctx, os.Args[3])
		check(err)
		key, err := base64.StdEncoding.DecodeString(os.Args[4])
		check(err)
		c, err := mfa.NewCipher(key)
		check(err)
		enc, err := c.Seal([]byte(os.Args[5]))
		check(err)
		check(st.SetMFARequired(ctx, u.ID, true))
		check(st.StoreMFAEnrollment(ctx, u.ID, enc, time.Now().UTC().Format(time.RFC3339), 0))
	case "totp":
		if len(os.Args) != 3 {
			die("usage: mfahelper totp <b32secret>")
		}
		code, err := totp.GenerateCode(os.Args[2], time.Now())
		check(err)
		fmt.Print(code)
	default:
		die("unknown subcommand " + os.Args[1])
	}
}

func open(path string) *store.Store {
	st, err := store.Open(path)
	check(err)
	return st
}

func check(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, "mfahelper:", msg)
	os.Exit(1)
}
