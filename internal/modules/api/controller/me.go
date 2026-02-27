package controller

import (
	"encoding/json"
	"net/http"
	"trade_bot/internal/modules/api/auth"
	"trade_bot/internal/modules/api/middleware"
)

type MeController struct{}

func NewMeController() *MeController { return &MeController{} }

func (c *MeController) Me(w http.ResponseWriter, r *http.Request) {
	claimsAny := r.Context().Value(middleware.ClaimsKey)
	claims, _ := claimsAny.(auth.Claims)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"tg_user_id": claims.TgUserID,
		"username":   claims.TgUsername,
	})
}
