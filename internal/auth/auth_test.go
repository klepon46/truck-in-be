package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestParseHS256AcceptsValidToken(t *testing.T) {
	secret := []byte("test-secret")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp":         time.Now().Add(time.Minute).Unix(),
		"permissions": []string{"unitlog:app:default:view"},
	})
	tokenString, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	claims, err := ParseHS256(tokenString, secret)
	if err != nil {
		t.Fatalf("ParseHS256() error = %v", err)
	}
	if !hasPermission(claims, "permissions", "unitlog:app:default:view") {
		t.Fatal("expected permission to be present")
	}
}

func TestParseHS256RejectsAnotherSigningMethod(t *testing.T) {
	secret := []byte("test-secret")
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.MapClaims{
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	tokenString, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := ParseHS256(tokenString, secret); err == nil {
		t.Fatal("ParseHS256() accepted an HS512 token")
	}
}
