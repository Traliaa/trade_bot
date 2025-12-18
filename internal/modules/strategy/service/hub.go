package service

//// Hub принимает свечи от MarketStream и генерит сигналы.
//type Hub struct {
//	mu sync.Mutex
//
//	emarsi   *EMARSI   // твоя существующая
//	donchian *Donchian // уже написали
//
//	out chan<- models.Signal
//}

//func NewHub(out chan models.Signal) *Hub {
//	return &Hub{
//		donchian: NewDonchian(DonchianConfig{
//			Period:   20,
//			TrendEma: 50,
//		}),
//		out: out,
//	}
//}

//func (h *Hub) OnCandle(ctx context.Context, tickSymbol, tf string, c models.CandleTick) {
//	h.mu.Lock()
//	defer h.mu.Unlock()
//
//	//// EMARSI
//	//if sig, ok := h.emarsi.OnCandle(tickSymbol, c); ok {
//	//	h.out <- models.Signal{
//	//		Symbol:    tickSymbol,
//	//		Timeframe: tf,
//	//		Side:      sig.Side,
//	//		Price:     sig.Price,
//	//		Strategy:  models.StrategyEMARSI,
//	//		Reason:    sig.Reason,
//	//	}
//	//}
//
//	// Donchian
//	ds := h.donchian.OnCandle(tickSymbol, c)
//	if ds.Side != models.SideNone {
//		h.out <- models.Signal{
//			InstID:   tickSymbol,
//			TF:       tf,
//			Side:     ds.Side,
//			Price:    ds.Price,
//			Strategy: models.StrategyDonchian,
//			Reason:   ds.Reason,
//		}
//	}
//
//	log.Printf("[EVAL] %s %s close=%.6f", tickSymbol, tf, c.Close)
//}
