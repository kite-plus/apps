package main

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
)

// newKey makes a key pair and publishes its public half in root, as
// cmd/keygen does; it returns the private half as the secret holds it.
func newKey(t *testing.T, root string) (minisign.PublicKey, string) {
	t.Helper()
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := pub.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, pubName), append(public, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	secret, err := priv.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	return pub, string(secret)
}

func TestOnlyThePublishedKeySigns(t *testing.T) {
	root := t.TempDir()
	_, secret := newKey(t, root)
	if key, err := signingKey(root, ""); key != nil || err != nil {
		t.Errorf("no secret = %v, %v", key, err)
	}
	if key, err := signingKey(root, secret+"\n"); key == nil || err != nil {
		t.Errorf("the published key = %v, %v", key, err)
	}
	other := t.TempDir()
	_, foreign := newKey(t, other)
	if _, err := signingKey(root, foreign); err == nil || !strings.Contains(err.Error(), "is not the key minisign.pub publishes") {
		t.Errorf("another key = %v", err)
	}
	if _, err := signingKey(root, "not a key"); err == nil {
		t.Error("a secret that is no key was taken")
	}
}

func TestTheSignatureFollowsTheIndex(t *testing.T) {
	root := t.TempDir()
	pub, secret := newKey(t, root)
	key, err := signingKey(root, secret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if wrote, err := signIndex(root, *key, now); wrote || err != nil {
		t.Errorf("no index yet: %v, %v", wrote, err)
	}
	writeFiles(t, root, map[string]string{"index.json": `{"format":1}` + "\n"})
	if wrote, err := signIndex(root, *key, now); !wrote || err != nil {
		t.Fatalf("first signature: %v, %v", wrote, err)
	}
	read := func() ([]byte, []byte) {
		data, _ := os.ReadFile(filepath.Join(root, "index.json"))
		sig, _ := os.ReadFile(filepath.Join(root, sigName))
		return data, sig
	}
	data, sig := read()
	if !minisign.Verify(pub, data, sig) || !strings.Contains(string(sig), "trusted comment: timestamp:1790856000\tfile:index.json") {
		t.Errorf("the signature does not verify:\n%s", sig)
	}
	if wrote, _ := signIndex(root, *key, now.Add(time.Hour)); wrote {
		t.Error("a signature that still matched was written again")
	}
	writeFiles(t, root, map[string]string{"index.json": `{"format":1,"apps":[]}` + "\n"})
	if wrote, _ := signIndex(root, *key, now); !wrote {
		t.Error("a changed index kept its old signature")
	}
	if data, sig := read(); !minisign.Verify(pub, data, sig) {
		t.Error("the new signature does not verify")
	}
}

func TestARunSignsTheIndexWithTheKeyItIsGiven(t *testing.T) {
	gh := newFakeGitHub(t)
	kite := fakeKite(t, nil)
	gh.release("kite-plus/kite", "v0.1.4", time.Now(), nil)
	gh.release("someone/kite-bar", "v1.0.0", time.Now(), map[string][]byte{"bar-1.0.0.zip": themePackage(t, "bar", "1.0.0")})
	root := gitRepo(t).root
	writeFiles(t, root, map[string]string{"themes/bar.yaml": "kind: theme\nid: bar\nrepo: someone/kite-bar\n"})
	pub, secret := newKey(t, root)

	out, err := run(context.Background(), options{root: root, kite: kite, repo: "kite-plus/apps", secret: secret}, gh.client())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "index.json"))
	sig, _ := os.ReadFile(filepath.Join(root, sigName))
	if !out.wrote || !out.signed || out.unsigned || !minisign.Verify(pub, data, sig) {
		t.Errorf("a run with a key: %+v", out)
	}

	out, err = run(context.Background(), options{root: root, kite: kite, repo: "kite-plus/apps"}, gh.client())
	if err != nil || out.signed || !out.unsigned || !strings.Contains(out.markdown(), "index.json is not signed") {
		t.Errorf("a run without one: %+v, %v", out, err)
	}

	_, foreign := newKey(t, t.TempDir())
	if _, err := run(context.Background(), options{root: root, kite: kite, secret: foreign}, gh.client()); err == nil {
		t.Error("a run signed with a key Kite does not carry")
	}
}
