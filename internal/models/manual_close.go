package models

import (
	"errors"
	"github.com/google/uuid"
)

var ErrCloseNotFound = errors.New("сделка или операция не найдена")
var ErrCloseConflict = errors.New("сделка закрыта или уже обрабатывается операция закрытия")

type ManualClose struct {
	RequestID uuid.UUID `json:"request_id"`
	TradeGUID uuid.UUID `json:"trade_guid"`
	Fraction  float64   `json:"fraction"`
	Size      float64   `json:"size"`
	OrderID   string    `json:"order_id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
}
