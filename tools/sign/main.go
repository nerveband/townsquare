// Command sign makes and uses the release signing key for self-updates.
//
//	go run ./tools/sign keygen KEYFILE   create a key (mode 600) and print its public key
//	go run ./tools/sign pub KEYFILE      print the public key
//	go run ./tools/sign sign KEYFILE FILE   write FILE.sig (base64 Ed25519)
//
// The private key never goes in the repository. Keep a backup: without it,
// existing installs can't verify new releases.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		fail("usage: sign keygen|pub KEYFILE | sign KEYFILE FILE")
	}
	switch os.Args[1] {
	case "keygen":
		if _, err := os.Stat(os.Args[2]); err == nil {
			fail(os.Args[2] + " already exists")
		}
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		check(err)
		check(os.WriteFile(os.Args[2], []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0o600))
		fmt.Println(pub(load(os.Args[2])))
	case "pub":
		fmt.Println(pub(load(os.Args[2])))
	case "sign":
		if len(os.Args) < 4 {
			fail("usage: sign sign KEYFILE FILE")
		}
		body, err := os.ReadFile(os.Args[3])
		check(err)
		sig := ed25519.Sign(load(os.Args[2]), body)
		check(os.WriteFile(os.Args[3]+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644))
	default:
		fail("unknown command " + os.Args[1])
	}
}

func load(path string) ed25519.PrivateKey {
	b, err := os.ReadFile(path)
	check(err)
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		fail(path + " is not a signing key")
	}
	return ed25519.NewKeyFromSeed(seed)
}

func pub(k ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "sign:", msg)
	os.Exit(1)
}
