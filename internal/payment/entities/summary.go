package entities

type Summary struct {
	TotalRequests int64   `json:"totalRequests"`
	TotalAmount   float64 `json:"totalAmount"`
}

type AggregatedSummary struct {
	Default  Summary `json:"default"`
	Fallback Summary `json:"fallback"`
}

func (s *AggregatedSummary) RoundAmount() {
	s.Default.TotalAmount = float64(int64(s.Default.TotalAmount*100+0.5)) / 100
	s.Fallback.TotalAmount = float64(int64(s.Fallback.TotalAmount*100+0.5)) / 100
}
