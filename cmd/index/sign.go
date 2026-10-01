package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aead.dev/minisign"
)

// sigName is the signature of the index, kept beside it. Kite fetches both
// and refuses an index its signature does not match.
const sigName = "index.json.minisig"

// pubName is the public key the index is signed with, as Kite carries it.
const pubName = "minisign.pub"

// signingKey reads the private key the index is signed with, and checks that
// it is the one pubName publishes: any other would sign an index every Kite
// refuses.
func signingKey(root, secret string) (*minisign.PrivateKey, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, nil
	}
	var key minisign.PrivateKey
	if err := key.UnmarshalText([]byte(secret)); err != nil {
		return nil, fmt.Errorf("MINISIGN_SECRET_KEY is not a minisign private key: %w", err)
	}
	pub, err := minisign.PublicKeyFromFile(filepath.Join(root, pubName))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pubName, err)
	}
	if !pub.Equal(key.Public()) {
		return nil, fmt.Errorf("MINISIGN_SECRET_KEY is not the key %s publishes", pubName)
	}
	return &key, nil
}

// signIndex keeps sigName a signature of index.json, and reports whether it
// wrote one: a signature that still matches the index is kept.
func signIndex(root string, key minisign.PrivateKey, now time.Time) (bool, error) {
	data, err := os.ReadFile(filepath.Join(root, "index.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	path := filepath.Join(root, sigName)
	if sig, err := os.ReadFile(path); err == nil && minisign.Verify(key.Public().(minisign.PublicKey), data, sig) {
		return false, nil
	}
	sig := minisign.SignWithComments(key, data,
		fmt.Sprintf("timestamp:%d\tfile:index.json", now.Unix()),
		"signature from the kite-plus/apps index key")
	return true, os.WriteFile(path, sig, 0o644)
}
