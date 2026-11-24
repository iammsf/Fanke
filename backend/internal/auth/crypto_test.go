package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndVerifyPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("strong-pass-12345")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	ok, err := VerifyPassword("strong-pass-12345", hash)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword valid = (%v, %v)", ok, err)
	}
	ok, err = VerifyPassword("wrong-pass-12345", hash)
	if err != nil || ok {
		t.Fatalf("VerifyPassword wrong = (%v, %v)", ok, err)
	}
}

func TestPasswordMaxLengthRejected(t *testing.T) {
	tooLong := strings.Repeat("a", MaxPasswordBytes+1)
	if _, err := HashPassword(tooLong); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("HashPassword(>128) = %v, want ErrPasswordTooLong", err)
	}
	hash, err := HashPassword("valid-pass-1234")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword(tooLong, hash); ok || !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("VerifyPassword(>128) = (%v, %v), want (false, ErrPasswordTooLong)", ok, err)
	}
}

func TestVerifyPasswordArgon2ParamUpperBounds(t *testing.T) {
	badHashes := []string{
		"argon2id$v=19$m=4194304,t=3,p=4$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo", // m > 256 MiB
		"argon2id$v=19$m=65536,t=20,p=4$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",  // t > 10
		"argon2id$v=19$m=65536,t=3,p=32$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",  // p > 16
		"argon2id$v=19$m=0,t=3,p=4$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",       // m == 0
		"argon2id$v=19$m=65536,t=0,p=4$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",   // t == 0
		"argon2id$v=19$m=65536,t=3,p=0$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",   // p == 0
	}
	for _, h := range badHashes {
		if ok, err := VerifyPassword("password1234", h); ok || !errors.Is(err, ErrHashFormat) {
			t.Errorf("VerifyPassword(%s) = (%v, %v), want (false, ErrHashFormat)", h, ok, err)
		}
	}
}
