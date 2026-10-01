package research

import "time"

type ExitDataset struct {
	Schema     int          `json:"schema"`
	Provenance string       `json:"provenance"`
	Manifest   ExitManifest `json:"manifest"`
	Samples    []ExitSample `json:"samples"`
}
type ExitManifest struct {
	Version string           `json:"version"`
	Config  ExitPolicyConfig `json:"config"`
	Costs   ExitCosts        `json:"costs"`
}
type ExitCosts struct {
	ExitFeeBPS  float64 `json:"exit_fee_bps"`
	SlippageBPS float64 `json:"slippage_bps"`
}
type ExitPolicyConfig struct {
	BETriggerR           float64 `json:"be_trigger_r"`
	BEOffsetR            float64 `json:"be_offset_r"`
	LockTriggerR         float64 `json:"lock_trigger_r"`
	LockOffsetR          float64 `json:"lock_offset_r"`
	TimeStopBars         int     `json:"time_stop_bars"`
	TimeStopMinCurrentR  float64 `json:"time_stop_min_current_r"`
	FixedTimeStopBars    int     `json:"fixed_time_stop_bars"`
	EarlyTimeStopBars    int     `json:"early_time_stop_bars"`
	EarlyTimeStopMinMFER float64 `json:"early_time_stop_min_mfe_r"`
	PartialEnabled       bool    `json:"partial_enabled"`
	PartialTriggerR      float64 `json:"partial_trigger_r"`
	PartialCloseFrac     float64 `json:"partial_close_frac"`
	StaleAfterBars       int     `json:"stale_after_bars"`
	StaleMinMFER         float64 `json:"stale_min_mfe_r"`
	StaleExitProfitR     float64 `json:"stale_exit_profit_r"`
	StaleNearBER         float64 `json:"stale_near_be_r"`
	StaleMaxAdverseR     float64 `json:"stale_max_adverse_r"`
	StaleGraceBars       int     `json:"stale_grace_bars"`
	StaleWorseByR        float64 `json:"stale_worse_by_r"`
	StaleTightenToBER    float64 `json:"stale_tighten_to_be_r"`
}
type ExitSample struct {
	ID                   string           `json:"id"`
	Symbol               string           `json:"symbol"`
	Side                 string           `json:"side"`
	Provenance           string           `json:"provenance"`
	ReconciliationStatus string           `json:"reconciliation_status"`
	FeeCurrency          string           `json:"fee_currency"`
	EntryAt              time.Time        `json:"entry_at"`
	EndAt                time.Time        `json:"end_at"`
	Entry                float64          `json:"entry"`
	InitialStop          float64          `json:"initial_stop"`
	InitialTarget        float64          `json:"initial_target"`
	Contracts            float64          `json:"contracts"`
	ContractValue        float64          `json:"contract_value"`
	TickSize             float64          `json:"tick_size"`
	LotSize              float64          `json:"lot_size"`
	MinSize              float64          `json:"min_size"`
	EntryFee             float64          `json:"entry_fee"`
	Config               ExitPolicyConfig `json:"config"`
	SpreadBPS            *float64         `json:"spread_bps"`
	AssumedSpreadBPS     float64          `json:"assumed_spread_bps"`
	FundingComplete      bool             `json:"funding_complete"`
	Bars                 []ExitBar        `json:"bars"`
	Funding              []Funding        `json:"funding"`
	EntryTail            *ExitBar         `json:"entry_tail"`
}
type ExitBar struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Source string    `json:"source"`
}
type ExitValidation struct {
	Accepted []ExitSample
	Excluded []ExitExclusion
	Warnings []string
}
type ExitExclusion struct {
	SampleID string `json:"sample_id"`
	Reason   string `json:"reason"`
}
