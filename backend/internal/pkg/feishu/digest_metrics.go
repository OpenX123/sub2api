package feishu

// DigestWindowMetrics contains exclusively aggregate values safe for analysis.
type DigestWindowMetrics struct {
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost_usd"`
	Errors   int64   `json:"errors"`
}
