// Command release-sign signs and verifies the SHA256SUMS file of a
// LinuxAdmin release (see docs/RELEASING.md).
//
//	release-sign -genkey FILE                 create a release key (prints the public key to embed)
//	release-sign -key FILE [-out SIG] SUMS    write SUMS.sig (or SIG)
//	release-sign -verify [-pub KEY] [-sig SIG] SUMS [-dir DIR]
//	                                          check the signature (default: embedded key) and,
//	                                          with -dir, the sha256 of every listed file in DIR
//
// The key file has the same format as plugin-sign's: base64 of the 64-byte
// ed25519 private key (a 32-byte seed is accepted too). In CI the key comes
// from the RELEASE_SIGNING_KEY secret written to a temporary 0600 file.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Fonlogen/LinuxAdmin/server/internal/signkey"
	"github.com/Fonlogen/LinuxAdmin/server/internal/update"
)

func main() {
	genkey := flag.String("genkey", "", "write a new private key to this file and print the public key")
	key := flag.String("key", "", "private key file used to sign")
	out := flag.String("out", "", "signature file to write (default: SUMS.sig)")
	verify := flag.Bool("verify", false, "verify instead of signing")
	pub := flag.String("pub", "", "base64 public key for -verify (default: the embedded release key)")
	sigPath := flag.String("sig", "", "signature file for -verify (default: SUMS.sig)")
	dir := flag.String("dir", "", "with -verify: also check the sha256 of every listed file in this folder")
	flag.Parse()

	switch {
	case *genkey != "":
		pk, err := signkey.GenerateFile(*genkey)
		check(err)
		fmt.Println("public key:", base64.StdEncoding.EncodeToString(pk))
	case *verify && flag.NArg() == 1:
		keys := update.TrustedKeys
		if *pub != "" {
			k, err := signkey.ParsePublic(*pub)
			check(err)
			keys = []ed25519.PublicKey{k}
		}
		sums, err := os.ReadFile(flag.Arg(0))
		check(err)
		sp := *sigPath
		if sp == "" {
			sp = flag.Arg(0) + ".sig"
		}
		sig, err := os.ReadFile(sp)
		check(err)
		check(update.VerifySums(sums, sig, keys))
		list, err := update.ParseSums(sums)
		check(err)
		if *dir != "" {
			names := make([]string, 0, len(list))
			for n := range list {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				check(update.VerifyFile(list, n, filepath.Join(*dir, n)))
				fmt.Println("ok", n)
			}
		}
		fmt.Println("signature ok")
	case *key != "" && flag.NArg() == 1:
		sk, err := signkey.Load(*key)
		check(err)
		sums, err := os.ReadFile(flag.Arg(0))
		check(err)
		if _, err := update.ParseSums(sums); err != nil {
			check(err)
		}
		dst := *out
		if dst == "" {
			dst = flag.Arg(0) + ".sig"
		}
		sig := update.SignSums(sums, sk)
		// Self-check against the public half of the key used.
		check(update.VerifySums(sums, sig, []ed25519.PublicKey{sk.Public().(ed25519.PublicKey)}))
		check(os.WriteFile(dst, sig, 0o644))
		fmt.Println("signed", flag.Arg(0), "->", dst)
		fmt.Println("public key:", base64.StdEncoding.EncodeToString(sk.Public().(ed25519.PublicKey)))
	default:
		fmt.Fprintln(os.Stderr, "usage: release-sign -genkey FILE | release-sign -key FILE [-out SIG] SUMS | release-sign -verify [-pub KEY] [-sig SIG] [-dir DIR] SUMS")
		os.Exit(2)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "release-sign:", err)
		os.Exit(1)
	}
}
