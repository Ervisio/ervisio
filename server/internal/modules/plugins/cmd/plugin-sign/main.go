// Command plugin-sign signs a LinuxAdmin plugin folder.
//
//	plugin-sign -genkey key.pem            create a new ed25519 key (prints the public key to embed)
//	plugin-sign -key key.pem <folder>      hash every file into manifest.json "files" and write manifest.sig
//	plugin-sign -verify [-pub BASE64] <folder>   check a signed folder
//
// The key file holds the base64 of the 64-byte ed25519 private key.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/plugins"
)

func main() {
	genkey := flag.String("genkey", "", "write a new private key to this file and print the public key")
	key := flag.String("key", "", "private key file used to sign")
	verify := flag.Bool("verify", false, "verify the folder instead of signing")
	pub := flag.String("pub", "", "base64 public key for -verify (default: the embedded team key)")
	flag.Parse()

	switch {
	case *genkey != "":
		pk, sk, err := ed25519.GenerateKey(rand.Reader)
		check(err)
		check(os.WriteFile(*genkey, []byte(base64.StdEncoding.EncodeToString(sk)+"\n"), 0o600))
		fmt.Println("public key:", base64.StdEncoding.EncodeToString(pk))
	case *verify:
		if flag.NArg() != 1 {
			usage()
		}
		keys := plugins.TrustedKeys
		if *pub != "" {
			b, err := base64.StdEncoding.DecodeString(*pub)
			if err != nil || len(b) != ed25519.PublicKeySize {
				check(fmt.Errorf("-pub is not a base64 ed25519 public key"))
			}
			keys = []ed25519.PublicKey{b}
		}
		s := plugins.CheckSignature(flag.Arg(0), keys)
		switch {
		case !s.Signed:
			fmt.Println("not signed")
			os.Exit(1)
		case !s.Verified:
			fmt.Println("INVALID:", s.Err)
			os.Exit(1)
		}
		fmt.Println("signature ok")
	case *key != "" && flag.NArg() == 1:
		b, err := os.ReadFile(*key)
		check(err)
		sk, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(sk) != ed25519.PrivateKeySize {
			check(fmt.Errorf("%s is not a plugin-sign key file", *key))
		}
		check(plugins.SignFolder(flag.Arg(0), ed25519.PrivateKey(sk)))
		fmt.Println("signed", flag.Arg(0))
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: plugin-sign -genkey FILE | plugin-sign -key FILE FOLDER | plugin-sign -verify [-pub KEY] FOLDER")
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "plugin-sign:", err)
		os.Exit(1)
	}
}
