package configtransfer

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"filippo.io/age"
)

const MaxPlaintext = 1 << 20
const MaxCiphertext = 2 << 20

var ErrInvalid = errors.New("invalid configuration transfer")

// Bound concurrent memory-hard operations on phones and server callers.
var cryptoWorker sync.Mutex

func Encrypt(raw, password []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaxPlaintext || len(password) == 0 || len(password) > 4096 {
		return nil, ErrInvalid
	}
	cryptoWorker.Lock()
	defer cryptoWorker.Unlock()
	r, err := age.NewScryptRecipient(string(password))
	if err != nil {
		return nil, ErrInvalid
	}
	r.SetWorkFactor(16)
	var out bytes.Buffer
	w, err := age.Encrypt(&out, r)
	if err != nil {
		return nil, ErrInvalid
	}
	if _, err = w.Write(raw); err != nil {
		return nil, ErrInvalid
	}
	if err = w.Close(); err != nil {
		return nil, ErrInvalid
	}
	return out.Bytes(), nil
}

func Decrypt(raw, password []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaxCiphertext || len(password) == 0 || len(password) > 4096 {
		return nil, ErrInvalid
	}
	cryptoWorker.Lock()
	defer cryptoWorker.Unlock()
	id, err := age.NewScryptIdentity(string(password))
	if err != nil {
		return nil, ErrInvalid
	}
	id.SetMaxWorkFactor(18)
	r, err := age.Decrypt(bytes.NewReader(raw), id)
	if err != nil {
		return nil, ErrInvalid
	}
	// Read through authenticated EOF; never return partial plaintext on failure.
	plain, err := io.ReadAll(io.LimitReader(r, MaxPlaintext+1))
	if err != nil || len(plain) == 0 || len(plain) > MaxPlaintext {
		clear(plain)
		return nil, ErrInvalid
	}
	return plain, nil
}
