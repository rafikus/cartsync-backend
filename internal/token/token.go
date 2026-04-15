package token

import (
	"errors"
	"os"
	"time"

	gjwt "github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID string `json:"userId"`
	gjwt.RegisteredClaims
}

func secret() []byte {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		s = "change-me-in-production"
	}
	return []byte(s)
}

func IssueToken(userID string) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: gjwt.RegisteredClaims{
			ExpiresAt: gjwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
			IssuedAt:  gjwt.NewNumericDate(time.Now()),
		},
	}
	return gjwt.NewWithClaims(gjwt.SigningMethodHS256, claims).SignedString(secret())
}

func ParseToken(tokenStr string) (*Claims, error) {
	token, err := gjwt.ParseWithClaims(tokenStr, &Claims{}, func(t *gjwt.Token) (any, error) {
		if _, ok := t.Method.(*gjwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret(), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
