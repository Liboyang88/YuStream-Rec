package domain

import (
	"github.com/golang-jwt/jwt/v4"
)

type JwtCustomClaims struct {
	Email string `json:"email"`
	ID    uint   `json:"id"`
	jwt.StandardClaims
}

type JwtCustomRefreshClaims struct {
	ID uint `json:"id"`
	jwt.StandardClaims
}
