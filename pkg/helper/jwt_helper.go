package helper

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ParseTokenExpiration extracts the expiration time from a JWT token without verifying the signature.
// Returns nil time if no expiration is found or token is not a JWT.
func ParseTokenExpiration(tokenString string) (*time.Time, error) {
	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	exp, err := claims.GetExpirationTime()
	if err != nil {
		return nil, fmt.Errorf("failed to get expiration time: %w", err)
	}

	if exp == nil {
		return nil, nil // No expiration
	}

	return &exp.Time, nil
}
