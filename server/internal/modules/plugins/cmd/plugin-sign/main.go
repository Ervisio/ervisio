// Command plugin-sign signs an Ervisio plugin folder.
//
//	plugin-sign -genkey key.pem            create a new ed25519 key (prints the public key to embed)
//	plugin-sign -key key.pem <folder>      hash every file into manifest.json "files" and write manifest.sig
//	plugin-sign -verify [-pub BASE64] <folder>   check a signed folder
//	plugin-sign -key key.pem -catalog catalog.json          write catalog.sig (marketplace catalog)
//	plugin-sign -verify [-pub BASE64] -catalog catalog.json check catalog.sig
//
// The key file holds the base64 of the 64-byte ed25519 private key (package
// signkey, shared with release-sign).
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/signkey"
)

func main() {
	genkey := flag.String("genkey", "", "write a new private key to this file and print the public key")
	key := flag.String("key", "", "private key file used to sign")
	verify := flag.Bool("verify", false, "verify the folder instead of signing")
	pub := flag.String("pub", "", "base64 public key for -verify (default: the embedded team key)")
	catalog := flag.String("catalog", "", "sign or verify this catalog.json (signature in the same folder, .json replaced by .sig)")
	flag.Parse()

	switch {
	case *catalog != "" && flag.NArg() == 0 && (*verify || *key != ""):
		if *verify {
			verifyCatalog(*catalog, *pub)
		} else {
			signCatalog(*catalog, *key)
		}
	case *genkey != "":
		pk, err := signkey.GenerateFile(*genkey)
		check(err)
		fmt.Println("public key:", base64.StdEncoding.EncodeToString(pk))
	case *verify:
		if flag.NArg() != 1 {
			usage()
		}
		verifyFolder(flag.Arg(0), *pub)
	case *key != "" && flag.NArg() == 1:
		sk, err := signkey.Load(*key)
		check(err)
		check(plugins.SignFolder(flag.Arg(0), sk))
		fmt.Println("signed", flag.Arg(0))
	default:
		usage()
	}
}

// catalogSigPath is where the signature of a catalog.json lives.
func catalogSigPath(catalog string) string {
	return strings.TrimSuffix(catalog, ".json") + ".sig"
}

// verifyKeys returns the keys to verify against: the one in pub, or the
// embedded team keys when pub is empty.
func verifyKeys(pub string) []ed25519.PublicKey {
	if pub == "" {
		return plugins.TrustedKeys
	}
	b, err := signkey.ParsePublic(pub)
	if err != nil {
		check(fmt.Errorf("-pub: %v", err))
	}
	return []ed25519.PublicKey{b}
}

func verifyCatalog(catalog, pub string) {
	data, err := os.ReadFile(catalog)
	check(err)
	keys := verifyKeys(pub)
	sig, err := os.ReadFile(catalogSigPath(catalog))
	check(err)
	if err := plugins.VerifyCatalog(data, sig, keys); err != nil {
		fmt.Println("INVALID:", err)
		os.Exit(1)
	}
	fmt.Println("catalog signature ok")
}

func signCatalog(catalog, keyFile string) {
	data, err := os.ReadFile(catalog)
	check(err)
	sigPath := catalogSigPath(catalog)
	sk, err := signkey.Load(keyFile)
	check(err)
	sig, err := plugins.SignCatalog(data, sk)
	check(err)
	check(os.WriteFile(sigPath, sig, 0o644))
	fmt.Println("signed", catalog, "->", sigPath)
}

func verifyFolder(folder, pub string) {
	s := plugins.CheckSignature(folder, verifyKeys(pub))
	switch {
	case !s.Signed:
		fmt.Println("not signed")
		os.Exit(1)
	case !s.Verified:
		fmt.Println("INVALID:", s.Err)
		os.Exit(1)
	}
	fmt.Println("signature ok")
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: plugin-sign -genkey FILE | plugin-sign -key FILE FOLDER | plugin-sign -verify [-pub KEY] FOLDER")
	fmt.Fprintln(os.Stderr, "       plugin-sign -key FILE -catalog catalog.json | plugin-sign -verify [-pub KEY] -catalog catalog.json")
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "plugin-sign:", err)
		os.Exit(1)
	}
}
