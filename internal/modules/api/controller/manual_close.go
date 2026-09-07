package controller

import (
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"math"
	"net/http"
	"trade_bot/internal/models"
)

func closeError(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	if errors.Is(err, models.ErrCloseNotFound) {
		code = http.StatusNotFound
	}
	if errors.Is(err, models.ErrCloseConflict) {
		code = http.StatusConflict
	}
	http.Error(w, err.Error(), code)
}

func (c *TradeController) ManualClose(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}
	guid, err := uuid.Parse(chi.URLParam(r, "guid"))
	if err != nil {
		http.Error(w, "invalid trade guid", 400)
		return
	}
	var body struct {
		RequestID uuid.UUID `json:"request_id"`
		Fraction  float64   `json:"fraction"`
	}
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.RequestID == uuid.Nil || body.Fraction <= 0 || body.Fraction > 1 || math.IsNaN(body.Fraction) {
		http.Error(w, "request_id and fraction (0,1] required", 400)
		return
	}
	result, err := c.r.ManualCloseTrade(r.Context(), userID, guid, body.RequestID, body.Fraction)
	if err != nil {
		closeError(w, err)
		return
	}
	writeJSON(w, result)
}
func (c *TradeController) ManualCloseStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := mustAuthUserID(w, r)
	if !ok {
		return
	}
	guid, err := uuid.Parse(chi.URLParam(r, "guid"))
	if err != nil {
		http.Error(w, "invalid trade guid", 400)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "requestID"))
	if err != nil {
		http.Error(w, "invalid request id", 400)
		return
	}
	result, err := c.r.ManualCloseStatus(r.Context(), userID, guid, id)
	if err != nil {
		closeError(w, err)
		return
	}
	writeJSON(w, result)
}
