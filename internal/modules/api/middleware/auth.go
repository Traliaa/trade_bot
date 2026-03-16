package middleware

import (
	"context"
	"net/http"
	"strings"
	"trade_bot/internal/modules/api/auth"
)

type ctxKey string

const ClaimsKey ctxKey = "claims"

func Auth(jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" || !strings.HasPrefix(h, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			tok := strings.TrimPrefix(h, "Bearer ")

			claims, err := auth.VerifyHS256(jwtSecret, tok)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, ClaimsKey, claims)
			ctx = context.WithValue(ctx, auth.UserIDContextKey{}, claims.TgUserID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
