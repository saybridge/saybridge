package metrics

import "testing"

func TestGaugeReadback(t *testing.T) {
	WSConnectionsActive.Set(0)
	IncWSConnection()
	IncWSConnection()
	if got := ActiveWSConnections(); got != 2 {
		t.Fatalf("ActiveWSConnections = %v, want 2", got)
	}
	DecWSConnection()
	if got := ActiveWSConnections(); got != 1 {
		t.Fatalf("ActiveWSConnections after Dec = %v, want 1", got)
	}
}

func TestHTTPErrorRateAndQuantiles(t *testing.T) {
	// 90 successful + 10 server-error requests → 10% error rate.
	for i := 0; i < 90; i++ {
		HTTPRequestsTotal.WithLabelValues("GET", "/x", "200").Inc()
		HTTPRequestDuration.WithLabelValues("GET", "/x").Observe(0.04)
	}
	for i := 0; i < 10; i++ {
		HTTPRequestsTotal.WithLabelValues("GET", "/x", "500").Inc()
		HTTPRequestDuration.WithLabelValues("GET", "/x").Observe(0.04)
	}

	if rate := HTTPErrorRate(); rate < 0.09 || rate > 0.11 {
		t.Fatalf("HTTPErrorRate = %v, want ~0.10", rate)
	}

	p50, p95, p99 := HTTPLatencyQuantiles()
	// All samples ~0.04s → quantiles should be positive and within the
	// [0.025, 0.05] bucket interpolation range.
	for name, v := range map[string]float64{"p50": p50, "p95": p95, "p99": p99} {
		if v <= 0 || v > 0.05 {
			t.Fatalf("%s latency = %v, want in (0, 0.05]", name, v)
		}
	}
	if p95 < p50 {
		t.Fatalf("p95 (%v) should be >= p50 (%v)", p95, p50)
	}
}

func TestQuantileFromBucketsEmpty(t *testing.T) {
	if got := quantileFromBuckets(nil, map[float64]uint64{}, 0, 0.5); got != 0 {
		t.Fatalf("empty buckets should yield 0, got %v", got)
	}
}
