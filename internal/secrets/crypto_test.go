package secrets

import "testing"

func TestSealOpenRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 3)
	}
	ct, err := Seal(key, "vault-plain-9.1")
	if err != nil {
		t.Fatal(err)
	}
	if ct == "" || ct == "vault-plain-9.1" {
		t.Fatal("ciphertext must not equal plaintext")
	}
	got, err := Open(key, ct)
	if err != nil {
		t.Fatal(err)
	}
	if got != "vault-plain-9.1" {
		t.Fatalf("got %q", got)
	}
}

func TestParseKeyHex(t *testing.T) {
	raw := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	k, err := parseKey(raw)
	if err != nil || len(k) != 32 {
		t.Fatalf("%v len=%d", err, len(k))
	}
}
