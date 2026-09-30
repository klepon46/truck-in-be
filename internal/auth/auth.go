package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const claimsContextKey = "auth_claims"

func Authenticate(secret []byte) gin.HandlerFunc {
	return func(context *gin.Context) {
		token, ok := strings.CutPrefix(context.GetHeader("Authorization"), "Bearer ")
		if !ok || token == "" {
			authError(context, http.StatusUnauthorized, "UNAUTHORIZED", "authentication is required")
			return
		}

		claims, err := ParseHS256(token, secret)
		if err != nil {
			authError(context, http.StatusUnauthorized, "UNAUTHORIZED", "authentication is required")
			return
		}
		context.Set(claimsContextKey, claims)
		context.Next()
	}
}

func RequirePermission(claimName, required string) gin.HandlerFunc {
	return func(context *gin.Context) {
		claimsValue, ok := context.Get(claimsContextKey)
		claims, claimsAreValid := claimsValue.(jwt.MapClaims)
		if !ok || !claimsAreValid || !hasPermission(claims, claimName, required) {
			authError(context, http.StatusForbidden, "FORBIDDEN", "required permission is missing")
			return
		}
		context.Next()
	}
}

func authError(context *gin.Context, status int, code, message string) {
	context.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func ClaimString(context *gin.Context, name string) (string, bool) {
	claimsValue, ok := context.Get(claimsContextKey)
	claims, claimsAreValid := claimsValue.(jwt.MapClaims)
	if !ok || !claimsAreValid {
		return "", false
	}
	value, ok := claims[name].(string)
	return value, ok && strings.TrimSpace(value) != ""
}

func ParseHS256(tokenString string, secret []byte) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func hasPermission(claims jwt.MapClaims, claimName, required string) bool {
	permissions, ok := claims[claimName]
	if !ok {
		return false
	}

	switch value := permissions.(type) {
	case string:
		return value == required
	case []any:
		for _, item := range value {
			permission, ok := item.(string)
			if ok && permission == required {
				return true
			}
		}
	}
	return false
}
