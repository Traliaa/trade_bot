package service

import "fmt"

// TradeError preserves OKX's machine-readable rejection, including through %w.
type TradeError struct{ Code, Msg, SCode, SMsg string }

func (e *TradeError) Error() string {
	return fmt.Sprintf("okx trade error: code=%s msg=%s sCode=%s sMsg=%s", e.Code, e.Msg, e.SCode, e.SMsg)
}

func (e *TradeError) EntryRestrictionCode() string {
	// Only an explicit account/instrument compliance rejection is permanent.
	// Sizing, leverage, balance, transport and generic errors must not blacklist.
	if e.SCode == "51155" {
		return e.SCode
	}
	return ""
}
