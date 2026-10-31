package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
)

// sealed is the plaintext inside an encrypted cookie.
type sealed struct {
	Exp int64           `json:"exp"`
	V   json.RawMessage `json:"v"`
}

// setCookie stores v in an encrypted cookie that expires after ttl. The cookie
// name is bound into the ciphertext, so values can't be swapped between cookies.
func (a *app) setCookie(w http.ResponseWriter, name string, v any, ttl time.Duration) {
	inner, _ := json.Marshal(v) // can't fail for our plain structs
	plain, _ := json.Marshal(sealed{Exp: time.Now().Add(ttl).Unix(), V: inner})
	nonce := make([]byte, a.aead.NonceSize())
	rand.Read(nonce)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    base64.RawURLEncoding.EncodeToString(a.aead.Seal(nonce, nonce, plain, []byte(name))),
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// readCookie decrypts cookie name into v. It returns false if the cookie is
// missing, tampered with or expired.
func (a *app) readCookie(r *http.Request, name string, v any) bool {
	c, err := r.Cookie(name)
	if err != nil {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(c.Value)
	n := a.aead.NonceSize()
	if err != nil || len(data) < n {
		return false
	}
	plain, err := a.aead.Open(nil, data[:n], data[n:], []byte(name))
	if err != nil {
		return false
	}
	var s sealed
	if json.Unmarshal(plain, &s) != nil || time.Now().Unix() > s.Exp {
		return false
	}
	return json.Unmarshal(s.V, v) == nil
}

func (a *app) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secure})
}
