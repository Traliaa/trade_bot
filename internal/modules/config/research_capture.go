package config

import (
	"regexp"
	"strconv"
)

type ResearchCaptureConfig struct {
	Enabled    bool
	ProtocolID string
}

var researchProtocolID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Invalid research configuration disables research only, never trading.
func ParseResearchCaptureConfig(lookup func(string) (string, bool)) (ResearchCaptureConfig, string) {
	value, ok := lookup("RESEARCH_CAPTURE_ENABLED")
	if !ok || value == "" {
		return ResearchCaptureConfig{}, ""
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return ResearchCaptureConfig{}, "invalid_enabled"
	}
	if !enabled {
		return ResearchCaptureConfig{}, ""
	}
	protocol, _ := lookup("RESEARCH_PROTOCOL_ID")
	if !researchProtocolID.MatchString(protocol) {
		return ResearchCaptureConfig{}, "invalid_protocol"
	}
	return ResearchCaptureConfig{Enabled: true, ProtocolID: protocol}, ""
}
