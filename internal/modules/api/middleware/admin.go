package middleware

import (
	"net/http"
	"trade_bot/internal/modules/api/auth"
)

// RequireAdmin must run after Auth. Never trust a user ID or role in the body.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := r.Context().Value(auth.UserIDContextKey{}).(int64)
		if !auth.IsAdmin(id) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
