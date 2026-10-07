package config

import "testing"

func TestResearchCaptureConfig(t *testing.T) {
	for _, tc := range []struct {
		enabled, protocol string
		want              bool
		bad               bool
	}{
		{"", "", false, false}, {"false", "", false, false}, {"true", "exit-study-1", true, false}, {"true", "", false, true}, {"wat", "x", false, true}, {"true", "unsafe/key", false, true},
	} {
		t.Run(tc.enabled+tc.protocol, func(t *testing.T) {
			cfg, code := ParseResearchCaptureConfig(func(k string) (string, bool) {
				if k == "RESEARCH_CAPTURE_ENABLED" {
					return tc.enabled, tc.enabled != ""
				}
				return tc.protocol, tc.protocol != ""
			})
			if cfg.Enabled != tc.want || (code != "") != tc.bad {
				t.Fatalf("bad config: %+v %s", cfg, code)
			}
		})
	}
}
