// Command plugin-sign signs a LinuxAdmin plugin folder.
//
//	plugin-sign -genkey key.pem            create a new ed25519 key (prints the public key to embed)
//	plugin-sign -key key.pem <folder>      hash every file into manifest.json "files" and write manifest.sig
//	plugin-sign -verify [-pub BASE64] <folder>   check a signed folder
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

	"github.com/Fonlogen/LinuxAdmin/server/internal/modules/plugins"
	"github.com/Fonlogen/LinuxAdmin/server/internal/signkey"
)

func main() {
	genkey := flag.String("genkey", "", "write a new private key to this file and print the public key")
	key := flag.String("key", "", "private key file used to sign")
	verify := flag.Bool("verify", false, "verify the folder instead of signing")
	pub := flag.String("pub", "", "base64 public key for -verify (default: the embedded team key)")
	flag.Parse()

	switch {
	case *genkey != "":
		pk, err := signkey.GenerateFile(*genkey)
		check(err)
		fmt.Println("public key:", base64.StdEncoding.EncodeToString(pk))
	case *verify:
		if flag.NArg() != 1 {
			usage()
		}
		keys := plugins.TrustedKeys
		if *pub != "" {
			b, err := signkey.ParsePublic(*pub)
			if err != nil {
				check(fmt.Errorf("-pub: %v", err))
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
		sk, err := signkey.Load(*key)
		check(err)
		check(plugins.SignFolder(flag.Arg(0), sk))
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
