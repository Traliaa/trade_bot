package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWarmupAcceptsOnlyClosedValidCandles(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	client := http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		fmt.Fprintf(w, `{"code":"0","data":[["%d","1","2","1","2","1","1","2","0"],["%d","NaN","2","1","2","1","1","2","1"],["%d","1","2","1","2","1","1","2","1"]]}`, now.UnixMilli(), now.Add(-time.Minute).UnixMilli(), now.Add(-2*time.Minute).UnixMilli())
		return w.Result(), nil
	})}
	s := &Service{client: client}
	cs, err := s.GetCandles(context.Background(), "TEST-USDT-SWAP", "1m", 3)
	if err != nil || len(cs) != 1 || !cs[0].Start.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("%+v %v", cs, err)
	}
}
