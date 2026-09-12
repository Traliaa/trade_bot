package research

import (
	"math/rand"
	"sort"
	"time"
)

type Interval struct {
	Lower      float64 `json:"lower"`
	Upper      float64 `json:"upper"`
	BlockDays  int     `json:"block_days"`
	Replicates int     `json:"replicates"`
	Method     string  `json:"method"`
}

// Resample blocks of account-level UTC daily net PnL, not individual correlated
// trades. The seed is fixed for reproducibility. This remains a model assumption.
func dailyInterval(trades []Trade, start, end time.Time) *Interval {
	first := start.UTC().Truncate(24 * time.Hour)
	if first.Before(start) {
		first = first.Add(24 * time.Hour)
	}
	days := int(end.UTC().Truncate(24*time.Hour).Sub(first) / (24 * time.Hour))
	if days < 30 || len(trades) < 100 {
		return nil
	}
	values := make([]float64, days)
	for _, t := range trades {
		day := int(t.CloseAt.UTC().Sub(first) / (24 * time.Hour))
		if day >= 0 && day < days {
			values[day] += t.Net
		}
	}
	rng := rand.New(rand.NewSource(20260912))
	means := make([]float64, 1000)
	for i := range means {
		n := 0
		sum := 0.
		for n < days {
			at := rng.Intn(days)
			for j := 0; j < 3 && n < days; j++ {
				sum += values[(at+j)%days]
				n++
			}
		}
		means[i] = sum / float64(days)
	}
	sort.Float64s(means)
	return &Interval{Lower: means[24], Upper: means[974], BlockDays: 3, Replicates: 1000, Method: "circular daily block bootstrap; 95% mean daily net USDT; conditional on sample/model"}
}
