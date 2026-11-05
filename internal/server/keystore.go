package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

// keyStore keeps the personal keys of people who chose to have them on all
// their devices: one encrypted file each in HOMENCLATURE_DATA_DIR. The file
// is that choice, so it's there even while they have no keys. Everyone else's
// keys stay in their browser (keys.go).
type keyStore struct {
	dir  string
	aead cipher.AEAD
}

// newKeyStore returns the store in dir, or nil if dir is empty: then keys are
// only kept in browsers. The files are encrypted with a key derived from the
// session secret, not the cookies' own.
func newKeyStore(dir, secret string) (*keyStore, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Fail now, not at someone's first save.
	f, err := os.CreateTemp(dir, ".check-*")
	if err != nil {
		return nil, fmt.Errorf("%s isn't writable: %w", dir, err)
	}
	f.Close()
	os.Remove(f.Name())

	key, err := hkdf.Key(sha256.New, []byte(secret), nil, "homenclature personal keys", 32)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(key)  // can't fail: key is 32 bytes
	aead, _ := cipher.NewGCM(block) // can't fail for AES
	return &keyStore{dir: dir, aead: aead}, nil
}

// path names sub's file by a hash, so any subject makes a safe file name.
func (ks *keyStore) path(sub string) string {
	h := sha256.Sum256([]byte(sub))
	return filepath.Join(ks.dir, hex.EncodeToString(h[:])+".keys")
}

// get returns sub's keys by link URL, and whether sub keeps them here. A
// file that can't be read counts as kept here without keys, so saving a key
// replaces it.
func (ks *keyStore) get(sub string) (map[string]string, bool) {
	data, err := os.ReadFile(ks.path(sub))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		log.Printf("keys: %v", err)
		return nil, true
	}
	var keys map[string]string
	n := ks.aead.NonceSize()
	if len(data) < n {
		log.Printf("keys: %s is too short", ks.path(sub))
		return nil, true
	}
	// The subject is bound into the ciphertext, so files can't be swapped.
	plain, err := ks.aead.Open(nil, data[:n], data[n:], []byte(sub))
	if err != nil || json.Unmarshal(plain, &keys) != nil {
		log.Printf("keys: can't decrypt %s; was HOMENCLATURE_SESSION_SECRET changed?", ks.path(sub))
		return nil, true
	}
	return keys, true
}

// put saves sub's keys, replacing what was there.
func (ks *keyStore) put(sub string, keys map[string]string) error {
	plain, _ := json.Marshal(keys) // can't fail for a map of strings
	nonce := make([]byte, ks.aead.NonceSize())
	rand.Read(nonce)
	// Write a new file and rename it over the old one, so a file is never
	// half written.
	f, err := os.CreateTemp(ks.dir, ".keys-*") // mode 0600
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // only left if something failed
	_, err = f.Write(ks.aead.Seal(nonce, nonce, plain, []byte(sub)))
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), ks.path(sub))
}

// remove deletes sub's keys: they're kept in the browser again.
func (ks *keyStore) remove(sub string) error {
	if err := os.Remove(ks.path(sub)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
