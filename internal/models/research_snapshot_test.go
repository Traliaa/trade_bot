package models

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestResearchSnapshotNotInTradeJSON(t *testing.T) {
	raw, err := json.Marshal(TradeRecord{ResearchEntrySnapshot: json.RawMessage(`{"capture_id":"PRIVATE_RESEARCH"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("research")) || bytes.Contains(raw, []byte("PRIVATE_RESEARCH")) {
		t.Fatal("research snapshot exposed via API")
	}
}
