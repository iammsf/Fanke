// Package secret 为数据库中的敏感字段(第三方密钥、SMTP 口令等)提供对称加密。
//
// 设计:
//   - AES-256-GCM;密钥由主密钥(RECOVERY_KEY)经 SHA-256 派生,不额外落盘;
//   - 密文形如 enc:v1:<base64url(nonce||ciphertext)>;无前缀视为历史明文,
//     读取时原样返回,写入时自动加密(便于平滑迁移);
//   - Box 为 nil 表示未启用加密(未配置主密钥的开发环境),所有方法退化为原文透传。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Prefix 密文前缀,用于区分历史明文。版本号便于日后换算法。
const Prefix = "enc:v1:"

const aad = "fanke-db-field-v1"

type Box struct {
	aead cipher.AEAD
}

// NewBox 由主密钥构造 Box;主密钥为空时返回 (nil, nil) 表示不加密。
func NewBox(master string) (*Box, error) {
	if strings.TrimSpace(master) == "" {
		return nil, nil
	}
	sum := sha256.Sum256([]byte("fanke-db-secret-v1|" + master))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal 加密明文字符串;空串、已加密串、未启用加密时原样返回。
func (b *Box) Seal(plain string) (string, error) {
	if b == nil || plain == "" || strings.HasPrefix(plain, Prefix) {
		return plain, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nil, nonce, []byte(plain), []byte(aad))
	buf := make([]byte, 0, len(nonce)+len(out))
	buf = append(buf, nonce...)
	buf = append(buf, out...)
	return Prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// Open 解密;非密文原样返回。密钥不匹配时返回错误,调用方应保留原值。
func (b *Box) Open(s string) (string, error) {
	if b == nil || !strings.HasPrefix(s, Prefix) {
		return s, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, Prefix))
	if err != nil {
		return "", err
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns+b.aead.Overhead() {
		return "", errors.New("secret: 密文长度非法")
	}
	pt, err := b.aead.Open(nil, raw[:ns], raw[ns:], []byte(aad))
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// Encrypted 判断字符串是否已加密。
func Encrypted(s string) bool { return strings.HasPrefix(s, Prefix) }
