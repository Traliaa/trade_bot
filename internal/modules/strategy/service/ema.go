package service

type emaState struct {
	period int
	alpha  float64
	value  float64
	warmup int
}

func newEMA(period int) emaState {
	if period <= 1 {
		period = 1
	}
	return emaState{
		period: period,
		alpha:  2.0 / (float64(period) + 1),
	}
}

func (e *emaState) Update(price float64) {
	if e.warmup == 0 {
		e.value = price
		e.warmup = 1
		return
	}
	e.value = e.alpha*price + (1-e.alpha)*e.value
	if e.warmup < e.period {
		e.warmup++
	}
}

func (e *emaState) Ready() bool    { return e.warmup >= e.period }
func (e *emaState) Value() float64 { return e.value }
