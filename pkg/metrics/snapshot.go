package metrics

import (
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// This file exposes read-back helpers so in-app surfaces (e.g. the admin
// analytics dashboard) can report live values from the same Prometheus metrics
// that are scraped externally — a single source of truth, no parallel counters.

// gaugeValue reads the current value of a Gauge.
func gaugeValue(g prometheus.Gauge) float64 {
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		return 0
	}
	return m.GetGauge().GetValue()
}

// ActiveWSConnections returns the current active WebSocket connection count.
func ActiveWSConnections() float64 { return gaugeValue(WSConnectionsActive) }

// LoadedPlugins returns the current loaded-plugin count.
func LoadedPlugins() float64 { return gaugeValue(PluginsLoaded) }

// HTTPErrorRate returns the fraction of HTTP requests that returned a 5xx
// status, in the range [0,1]. Returns 0 when no requests have been recorded.
func HTTPErrorRate() float64 {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return 0
	}
	var total, errors float64
	for _, mf := range mfs {
		if mf.GetName() != namespace+"_http_requests_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			v := m.GetCounter().GetValue()
			total += v
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "status" && strings.HasPrefix(lp.GetValue(), "5") {
					errors += v
				}
			}
		}
	}
	if total == 0 {
		return 0
	}
	return errors / total
}

// HTTPLatencyQuantiles estimates p50/p95/p99 HTTP request latency (in seconds)
// from the aggregated request-duration histogram across all routes. Estimates
// are interpolated from histogram buckets, so they are approximate — good
// enough for an at-a-glance dashboard; use the raw histogram in Grafana/PromQL
// for precise quantiles.
func HTTPLatencyQuantiles() (p50, p95, p99 float64) {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return 0, 0, 0
	}

	cumulative := map[float64]uint64{}
	var totalCount uint64
	found := false

	for _, mf := range mfs {
		if mf.GetName() != namespace+"_http_request_duration_seconds" {
			continue
		}
		found = true
		for _, m := range mf.GetMetric() {
			h := m.GetHistogram()
			if h == nil {
				continue
			}
			totalCount += h.GetSampleCount()
			for _, b := range h.GetBucket() {
				cumulative[b.GetUpperBound()] += b.GetCumulativeCount()
			}
		}
	}
	if !found || totalCount == 0 {
		return 0, 0, 0
	}

	bounds := make([]float64, 0, len(cumulative))
	for ub := range cumulative {
		bounds = append(bounds, ub)
	}
	sort.Float64s(bounds)

	return quantileFromBuckets(bounds, cumulative, totalCount, 0.50),
		quantileFromBuckets(bounds, cumulative, totalCount, 0.95),
		quantileFromBuckets(bounds, cumulative, totalCount, 0.99)
}

// quantileFromBuckets does linear interpolation within the bucket that contains
// the target rank, mirroring how Prometheus's histogram_quantile works.
func quantileFromBuckets(bounds []float64, cumulative map[float64]uint64, total uint64, q float64) float64 {
	if len(bounds) == 0 {
		return 0
	}
	rank := q * float64(total)

	var prevBound float64
	var prevCount uint64
	for _, ub := range bounds {
		count := cumulative[ub]
		if float64(count) >= rank {
			// +Inf bucket: cannot interpolate, return the last finite bound.
			if ub == bounds[len(bounds)-1] && len(bounds) > 1 {
				// fall through to interpolation below if a finite bound exists
			}
			bucketCount := count - prevCount
			if bucketCount == 0 {
				return prevBound
			}
			ratio := (rank - float64(prevCount)) / float64(bucketCount)
			return prevBound + ratio*(ub-prevBound)
		}
		prevBound = ub
		prevCount = count
	}
	return bounds[len(bounds)-1]
}
