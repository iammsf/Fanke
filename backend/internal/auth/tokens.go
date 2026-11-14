package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	Access  string
	Refresh string
	JTI     string
}

type Signer struct {
	key           []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
}

func NewSigner(secret string, accessTTL, refreshTTL time.Duration) *Signer {
	return &Signer{key: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (s *Signer) TTL() (time.Duration, time.Duration) { return s.accessTTL, s.refreshTTL }

func (s *Signer) Issue(userID int64, role string) (*TokenPair, error) {
	jti := newJTI()
	now := time.Now()
	access, err := s.sign(CustomClaimsFor(userID, role, "access", now, now.Add(s.accessTTL), ""))
	if err != nil {
		return nil, err
	}
	refresh, err := s.sign(CustomClaimsFor(userID, role, "refresh", now, now.Add(s.refreshTTL), jti))
	if err != nil {
		return nil, err
	}
	return &TokenPair{Access: access, Refresh: refresh, JTI: jti}, nil
}

func CustomClaimsFor(userID int64, role, kind string, now, exp time.Time, jti string) Claims {
	return Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   kind,
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "fanke",
		},
	}
}

func (s *Signer) sign(c Claims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.key)
}

var ErrBadToken = errors.New("invalid or expired token")

func (s *Signer) Parse(tokenStr string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrBadToken
		}
		return s.key, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !tok.Valid {
		return nil, ErrBadToken
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok {
		return nil, ErrBadToken
	}
	return claims, nil
}

func newJTI() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
