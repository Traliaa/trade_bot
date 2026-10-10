//go:build linux || darwin

package collect

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
)

type Protocol struct {
	Format       string    `json:"format"`
	ID           string    `json:"protocol_id"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	HorizonHours int       `json:"horizon_hours"`
	MaxPages     int       `json:"max_pages"`
	MaxBytes     int64     `json:"max_bytes"`
}

func (p Protocol) valid() bool {
	return p.Format == "prospective-exit-protocol-v1" && entryProtocol.MatchString(p.ID) && p.Start.Year() >= 1970 && p.End.Year() <= 9999 && p.Start.Nanosecond()%int(time.Millisecond) == 0 && p.End.Equal(p.Start.Add(28*24*time.Hour)) && p.HorizonHours == 72 && p.MaxPages == 200 && p.MaxBytes > 0 && p.MaxBytes <= 1<<30
}

// strictRecord also rejects case aliases and missing fields by comparing the
// decoded representation to the exact schema, not merely json.Unmarshal.
func strictRecord(b []byte, out any) error {
	fail := errors.New("research_record_invalid")
	d := json.NewDecoder(bytes.NewReader(b))
	if uniqueJSON(d, 0) != nil {
		return fail
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return fail
	}
	canonical, err := json.Marshal(out)
	if err != nil {
		return fail
	}
	var a, c any
	da := json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	dc := json.NewDecoder(bytes.NewReader(canonical))
	dc.UseNumber()
	if da.Decode(&a) != nil || dc.Decode(&c) != nil || !sameJSON(a, c) {
		return fail
	}
	return nil
}

// Refs exposes only checked content hashes, never filesystem paths or private IDs.
func (s *Store) Refs() ([]string, error) {
	if s == nil {
		return nil, errStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return nil, errStore
	}
	f, err := s.root.Open(".")
	if err != nil {
		return nil, errStore
	}
	defer f.Close()
	entries, err := f.ReadDir(s.maxFiles + 1)
	if err != nil && err != io.EOF || len(entries) > s.maxFiles {
		return nil, errStore
	}
	refs := []string{}
	for _, e := range entries {
		ref := strings.TrimSuffix(e.Name(), ".json")
		if refPattern.MatchString(ref) && e.Name() == ref+".json" {
			if _, err := s.get(ref); err != nil {
				return nil, err
			}
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)
	return refs, nil
}
func LoadProtocol(s *Store, ref string) (Protocol, error) {
	var p Protocol
	if s == nil {
		return p, errStore
	}
	b, err := s.Get(ref)
	if err != nil {
		return p, err
	}
	if strictRecord(b, &p) != nil || !p.valid() {
		return Protocol{}, errors.New("research_protocol_invalid")
	}
	return p, nil
}
func RegisterProtocol(s *Store, p Protocol, now time.Time) (string, error) {
	if !p.valid() || now.IsZero() {
		return "", errors.New("research_protocol_invalid")
	}
	p.Start = p.Start.UTC()
	p.End = p.End.UTC()
	refs, err := s.Refs()
	if err != nil {
		return "", err
	}
	var existing string
	for _, ref := range refs {
		b, err := s.Get(ref)
		if err != nil {
			return "", err
		}
		var tag struct {
			Format string `json:"format"`
		}
		if json.Unmarshal(b, &tag) != nil {
			return "", errStore
		}
		if tag.Format != "prospective-exit-protocol-v1" {
			continue
		}
		old, err := LoadProtocol(s, ref)
		if err != nil {
			return "", err
		}
		if old != p {
			return "", errors.New("research_protocol_conflict")
		}
		existing = ref
	}
	if existing != "" {
		return existing, nil
	}
	if p.Start.Before(now) {
		return "", errors.New("research_protocol_backdated")
	}
	b, _ := json.Marshal(p)
	return s.Put(b)
}
