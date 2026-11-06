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
// their devices: one encrypted file each in HOMENCLATURE_DATA_DIR. Everyone
// else's keys stay in their browser (keys.go).
type keyStore struct {
	dir  string
	aead cipher.AEAD
}

// storedKeys is a person's file. It records their choice, so it stays while
// they have no keys, and after they move them back to a browser, to keep
// Epoch.
type storedKeys struct {
	OnServer bool              `json:"on_server"`       // false: they moved them back to a browser
	Epoch    string            `json:"epoch,omitempty"` // see userKeys.Epoch
	Keys     map[string]string `json:"keys,omitempty"`
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

// get returns sub's file, and whether there is one. A file that can't be
// read counts as keys kept here, without any, so saving a key replaces it.
func (ks *keyStore) get(sub string) (storedKeys, bool) {
	data, err := os.ReadFile(ks.path(sub))
	if errors.Is(err, fs.ErrNotExist) {
		return storedKeys{}, false
	}
	unreadable := storedKeys{OnServer: true}
	if err != nil {
		log.Printf("keys: %v", err)
		return unreadable, true
	}
	n := ks.aead.NonceSize()
	if len(data) < n {
		log.Printf("keys: %s is too short", ks.path(sub))
		return unreadable, true
	}
	// The subject is bound into the ciphertext, so files can't be swapped.
	var stored storedKeys
	plain, err := ks.aead.Open(nil, data[:n], data[n:], []byte(sub))
	if err != nil || json.Unmarshal(plain, &stored) != nil {
		log.Printf("keys: can't decrypt %s; was HOMENCLATURE_SESSION_SECRET changed?", ks.path(sub))
		return unreadable, true
	}
	return stored, true
}

// put saves sub's file, replacing what was there.
func (ks *keyStore) put(sub string, stored storedKeys) error {
	plain, _ := json.Marshal(stored) // can't fail for this struct
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
