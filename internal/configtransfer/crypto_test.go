package configtransfer

import (
	"bytes"
	"testing"
)

func TestEncryption(t *testing.T) {
	raw := []byte(`{"format":"quic-lab-config"}`)
	a, err := Encrypt(raw, []byte("test passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(raw, []byte("test passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) || bytes.Contains(a, raw) {
		t.Fatal("encryption must conceal and randomize payload")
	}
	got, err := Decrypt(a, []byte("test passphrase"))
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("roundtrip failed", err)
	}
	for _, input := range [][]byte{a[:len(a)-1], append(append([]byte{}, a...), 1)} {
		got, err := Decrypt(input, []byte("test passphrase"))
		if err == nil || got != nil {
			t.Fatal("invalid stream returned plaintext")
		}
	}
	if got, err := Decrypt(a, []byte("wrong")); err == nil || got != nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := Encrypt(bytes.Repeat([]byte{1}, (1<<20)+1), []byte("pass")); err == nil {
		t.Fatal("oversize plaintext accepted")
	}
	if _, err := Encrypt(raw, nil); err == nil {
		t.Fatal("empty password accepted")
	}
	if _, err := Decrypt(bytes.Repeat([]byte{1}, (2<<20)+1), []byte("pass")); err == nil {
		t.Fatal("oversize ciphertext accepted")
	}
}

func TestRejectExcessiveWorkFactor(t *testing.T) {
	raw := []byte("age-encryption.org/v1\n-> scrypt AAAAAAAAAAAAAAAAAAAAAA 30\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n--- AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n")
	if p, e := Decrypt(raw, []byte("pass")); e == nil || p != nil {
		t.Fatal("excessive work accepted")
	}
}
