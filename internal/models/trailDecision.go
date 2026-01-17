package models

type TrailDecision struct {
	NewSL     float64
	MoveSL    bool
	Close     bool
	Reason    string
	CloseSize float64 // ✅ частичное закрытие

}
