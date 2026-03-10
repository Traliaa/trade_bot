package models

type TrailDecision struct {
	MoveSL bool
	NewSL  float64

	Close bool

	CloseSize float64

	Reason CloseReason
	Note   string

	MoveSLAfterPartial bool
	NewSLAfterPartial  float64
}
