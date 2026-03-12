package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func mustAuthUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, err := authUserIDFromContext(r.Context())
	if err != nil || userID <= 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return 0, false
	}
	return userID, true
}

func authUserIDFromContext(ctx context.Context) (int64, error) {
	v := ctx.Value(userIDContextKey{})
	if v == nil {
		return 0, errors.New("user id not found in context")
	}

	id, ok := v.(int64)
	if !ok || id <= 0 {
		return 0, errors.New("invalid user id in context")
	}

	return id, nil
}

type userIDContextKey struct{}

func parsePositiveInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n <= 0 {
		return 0, errors.New("bad positive int")
	}
	return n, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
