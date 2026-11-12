package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func saveSession(t *testing.T, authKey, encryptionKey string) error {
	t.Helper()
	store, err := newCookieStore(authKey, encryptionKey)
	if err != nil {
		t.Fatalf("newCookieStore(%q, %q) = %v", authKey, encryptionKey, err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	session, _ := store.Get(req, "s")
	session.Values["k"] = "v"
	return session.Save(req, httptest.NewRecorder())
}

// TestCookieStoreEmptyEncryptionKeyMeansNoEncryption pins the zero-length AES
// key: Save failed for every session when only the auth key was configured.
func TestCookieStoreEmptyEncryptionKeyMeansNoEncryption(t *testing.T) {
	if err := saveSession(t, "auth-secret", ""); err != nil {
		t.Errorf("session.Save() = %v", err)
	}
}

func TestCookieStoreValidEncryptionKeyLengths(t *testing.T) {
	for _, n := range []int{16, 24, 32} {
		if err := saveSession(t, "auth-secret", strings.Repeat("k", n)); err != nil {
			t.Errorf("encryption key of %d bytes: session.Save() = %v", n, err)
		}
	}
}

func TestCookieStoreRandomKeysWithoutAuthKey(t *testing.T) {
	if err := saveSession(t, "", ""); err != nil {
		t.Errorf("session.Save() = %v", err)
	}
}

func TestCookieStoreRejectsBadEncryptionKeyLength(t *testing.T) {
	for _, n := range []int{1, 15, 17, 31, 33} {
		_, err := newCookieStore("auth-secret", strings.Repeat("k", n))
		if err == nil || !strings.Contains(err.Error(), "cookie-encryption-key") {
			t.Errorf("%d bytes: err = %v, want an error naming cookie-encryption-key", n, err)
		}
	}
}
