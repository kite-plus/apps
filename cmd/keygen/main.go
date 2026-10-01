// Command keygen makes the key pair the index is signed with.
//
// The private key goes to the Index workflow alone, as the
// MINISIGN_SECRET_KEY secret, and to a safe place of the maintainer's; the
// public key is written to minisign.pub and built into Kite, which refuses
// an index it does not sign. A new key therefore needs a Kite release that
// carries it before the index is signed with it.
package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"aead.dev/minisign"
)

func main() {
	out := flag.String("out", "", "the file to write the private key to; it must not exist yet")
	pubFile := flag.String("pub", "minisign.pub", "the file to write the public key to")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "keygen: say where the private key goes with -out")
		os.Exit(2)
	}
	if err := keygen(*out, *pubFile); err != nil {
		fmt.Fprintln(os.Stderr, "keygen:", err)
		os.Exit(1)
	}
}

func keygen(out, pubFile string) error {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	secret, err := priv.MarshalText()
	if err != nil {
		return err
	}
	public, err := pub.MarshalText()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(secret, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(pubFile, append(public, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("private key: %s\npublic key:  %s (in %s)\n", out, pub, pubFile)
	return nil
}
