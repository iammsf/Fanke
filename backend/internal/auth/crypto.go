// Package auth 提供 argon2id 口令哈希与 JWT 签发/校验。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime        = 3
	argonMemory      = 64 * 1024 // KiB = 64MB
	argonThreads     = 4
	argonKeyLen      = 32
	saltLen          = 16
	MaxPasswordBytes = 128
	maxVerifyMemory  = 256 * 1024 // KiB = 256MB
	maxVerifyTime    = 10
	maxVerifyThreads = 16
	maxVerifySaltLen = 64
	maxVerifyKeyLen  = 128
)

var (
	ErrHashFormat      = errors.New("invalid password hash format")
	ErrPasswordTooLong = errors.New("password too long")
)

// HashPassword 返回形如 argon2id$v=19$m=65536,t=3,p=4$salt$hash 的字符串。
func HashPassword(password string) (string, error) {
	if len(password) > MaxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword 校验 argon2id 哈希,恒定时间比较。
func VerifyPassword(password, encoded string) (bool, error) {
	if len(password) > MaxPasswordBytes {
		return false, ErrPasswordTooLong
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false, ErrHashFormat
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, ErrHashFormat
	}
	if m == 0 || m > maxVerifyMemory || t == 0 || t > maxVerifyTime || p == 0 || p > maxVerifyThreads {
		return false, ErrHashFormat
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) == 0 || len(salt) > maxVerifySaltLen {
		return false, ErrHashFormat
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(want) == 0 || len(want) > maxVerifyKeyLen {
		return false, ErrHashFormat
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
