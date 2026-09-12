package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"trade_bot/internal/modules/api/auth"
)

func TestAdminRequiresServerIdentity(t *testing.T) {
	for _, tc := range []struct {
		configured string
		id         int64
		allowed    bool
	}{
		{"123", 123, true}, {"123", 124, false}, {"123", 0, false},
		{"", 123, false}, {"invalid", 123, false}, {"-1", -1, false},
	} {
		t.Run(tc.configured, func(t *testing.T) {
			t.Setenv("ADMIN_TELEGRAM_USER_ID", tc.configured)
			called := false
			h := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
			r := httptest.NewRequest("POST", "/?user_id=123&is_admin=true", nil)
			r = r.WithContext(context.WithValue(r.Context(), auth.UserIDContextKey{}, tc.id))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if called != tc.allowed || !tc.allowed && w.Code != http.StatusForbidden {
				t.Fatalf("called=%v status=%d", called, w.Code)
			}
		})
	}
}
