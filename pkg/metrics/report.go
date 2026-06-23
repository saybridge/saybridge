package metrics

import (
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Snapshot is a curated, structured view of the live metrics, suitable for an
// in-app admin dashboard. It is computed from the same Prometheus registry that
// is scraped at /metrics, so it never diverges from the external data.
type Snapshot struct {
	HTTP          HTTPSnapshot          `json:"http"`
	WebSocket     WebSocketSnapshot     `json:"websocket"`
	Messages      MessagesSnapshot      `json:"messages"`
	Auth          map[string]float64    `json:"auth"`          // result -> count
	Notifications NotificationsSnapshot `json:"notifications"`
	AI            AISnapshot            `json:"ai"`
	Plugins       PluginsSnapshot       `json:"plugins"`
	Database      DatabaseSnapshot      `json:"database"`
	Runtime       RuntimeSnapshot       `json:"runtime"`
}

type HTTPSnapshot struct {
	TotalRequests    float64            `json:"total_requests"`
	RequestsByStatus map[string]float64 `json:"requests_by_status"` // status class ("2xx"...) -> count
	ErrorRate        float64            `json:"error_rate"`
	InFlight         float64            `json:"in_flight"`
	LatencyP50       float64            `json:"latency_p50"`
	LatencyP95       float64            `json:"latency_p95"`
	LatencyP99       float64            `json:"latency_p99"`
}

type WebSocketSnapshot struct {
	ActiveConnections float64 `json:"active_connections"`
	MessagesInbound   float64 `json:"messages_inbound"`
	MessagesOutbound  float64 `json:"messages_outbound"`
}

type MessagesSnapshot struct {
	Total  float64            `json:"total"`
	ByType map[string]float64 `json:"by_type"`
}

type NotificationsSnapshot struct {
	Total     float64            `json:"total"`
	ByOutcome map[string]float64 `json:"by_outcome"`
}

type AISnapshot struct {
	Requests     float64            `json:"requests"`
	Errors       float64            `json:"errors"`
	InputTokens  float64            `json:"input_tokens"`
	OutputTokens float64            `json:"output_tokens"`
	ByProvider   map[string]float64 `json:"by_provider"`
}

type PluginsSnapshot struct {
	Loaded     float64 `json:"loaded"`
	HookErrors float64 `json:"hook_errors"`
}

type DatabaseSnapshot struct {
	OpenConnections float64 `json:"open_connections"`
	InUse           float64 `json:"in_use"`
	Idle            float64 `json:"idle"`
	WaitCount       float64 `json:"wait_count"`
}

type RuntimeSnapshot struct {
	Goroutines    float64 `json:"goroutines"`
	HeapAllocByes float64 `json:"heap_alloc_bytes"`
	ResidentBytes float64 `json:"resident_memory_bytes"`
}

// Gather computes a full metrics Snapshot from the default Prometheus registry.
func Gather() Snapshot {
	fams := gatherMap()
	var s Snapshot

	// HTTP
	httpReqs := fams[namespace+"_http_requests_total"]
	s.HTTP.TotalRequests = mfCounterSum(httpReqs)
	s.HTTP.RequestsByStatus = statusClasses(httpReqs)
	s.HTTP.ErrorRate = ratio(s.HTTP.RequestsByStatus["5xx"], s.HTTP.TotalRequests)
	s.HTTP.InFlight = mfGaugeFirst(fams[namespace+"_http_requests_in_flight"])
	s.HTTP.LatencyP50, s.HTTP.LatencyP95, s.HTTP.LatencyP99 =
		histogramQuantiles(fams[namespace+"_http_request_duration_seconds"], 0.50, 0.95, 0.99)

	// WebSocket
	s.WebSocket.ActiveConnections = mfGaugeFirst(fams[namespace+"_ws_connections_active"])
	wsMsgs := mfCounterByLabel(fams[namespace+"_ws_messages_total"], "direction")
	s.WebSocket.MessagesInbound = wsMsgs["inbound"]
	s.WebSocket.MessagesOutbound = wsMsgs["outbound"]

	// Messages
	msgs := fams[namespace+"_messages_total"]
	s.Messages.Total = mfCounterSum(msgs)
	s.Messages.ByType = mfCounterByLabel(msgs, "msg_type")

	// Auth
	s.Auth = mfCounterByLabel(fams[namespace+"_auth_attempts_total"], "result")

	// Notifications
	notifs := fams[namespace+"_notifications_total"]
	s.Notifications.Total = mfCounterSum(notifs)
	s.Notifications.ByOutcome = mfCounterByLabel(notifs, "outcome")

	// AI
	aiReqs := fams[namespace+"_ai_requests_total"]
	s.AI.Requests = mfCounterSum(aiReqs)
	s.AI.Errors = mfCounterByLabel(aiReqs, "outcome")["error"]
	s.AI.ByProvider = mfCounterByLabel(aiReqs, "provider")
	aiTokens := mfCounterByLabel(fams[namespace+"_ai_tokens_total"], "kind")
	s.AI.InputTokens = aiTokens["input"]
	s.AI.OutputTokens = aiTokens["output"]

	// Plugins
	s.Plugins.Loaded = mfGaugeFirst(fams[namespace+"_plugins_loaded"])
	s.Plugins.HookErrors = mfCounterSum(fams[namespace+"_plugin_hook_errors_total"])

	// Database — read live pool stats directly when available.
	if registeredDB != nil {
		st := registeredDB.Stats()
		s.Database = DatabaseSnapshot{
			OpenConnections: float64(st.OpenConnections),
			InUse:           float64(st.InUse),
			Idle:            float64(st.Idle),
			WaitCount:       float64(st.WaitCount),
		}
	}

	// Runtime (from the standard Go / process collectors)
	s.Runtime.Goroutines = mfGaugeFirst(fams["go_goroutines"])
	s.Runtime.HeapAllocByes = mfGaugeFirst(fams["go_memstats_heap_alloc_bytes"])
	s.Runtime.ResidentBytes = mfGaugeFirst(fams["process_resident_memory_bytes"])

	return s
}

// ── gather helpers ──────────────────────────────────────────────────────────

func gatherMap() map[string]*dto.MetricFamily {
	out := map[string]*dto.MetricFamily{}
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return out
	}
	for _, mf := range mfs {
		out[mf.GetName()] = mf
	}
	return out
}

func mfCounterSum(mf *dto.MetricFamily) float64 {
	if mf == nil {
		return 0
	}
	var s float64
	for _, m := range mf.GetMetric() {
		s += m.GetCounter().GetValue()
	}
	return s
}

func mfCounterByLabel(mf *dto.MetricFamily, label string) map[string]float64 {
	res := map[string]float64{}
	if mf == nil {
		return res
	}
	for _, m := range mf.GetMetric() {
		v := m.GetCounter().GetValue()
		for _, lp := range m.GetLabel() {
			if lp.GetName() == label {
				res[lp.GetValue()] += v
			}
		}
	}
	return res
}

func mfGaugeFirst(mf *dto.MetricFamily) float64 {
	if mf == nil || len(mf.GetMetric()) == 0 {
		return 0
	}
	return mf.GetMetric()[0].GetGauge().GetValue()
}

// statusClasses buckets an http_requests_total family into status classes
// ("2xx", "4xx", "5xx", ...).
func statusClasses(mf *dto.MetricFamily) map[string]float64 {
	res := map[string]float64{}
	if mf == nil {
		return res
	}
	for _, m := range mf.GetMetric() {
		v := m.GetCounter().GetValue()
		for _, lp := range m.GetLabel() {
			if lp.GetName() == "status" && len(lp.GetValue()) > 0 {
				res[string(lp.GetValue()[0])+"xx"] += v
			}
		}
	}
	return res
}

func ratio(part, total float64) float64 {
	if total == 0 {
		return 0
	}
	return part / total
}

// histogramQuantiles aggregates all series of a histogram family and estimates
// the requested quantiles via bucket interpolation.
func histogramQuantiles(mf *dto.MetricFamily, qs ...float64) (p50, p95, p99 float64) {
	out := make([]float64, len(qs))
	if mf == nil {
		return 0, 0, 0
	}

	cumulative := map[float64]uint64{}
	var total uint64
	for _, m := range mf.GetMetric() {
		h := m.GetHistogram()
		if h == nil {
			continue
		}
		total += h.GetSampleCount()
		for _, b := range h.GetBucket() {
			cumulative[b.GetUpperBound()] += b.GetCumulativeCount()
		}
	}
	if total == 0 {
		return 0, 0, 0
	}

	bounds := make([]float64, 0, len(cumulative))
	for ub := range cumulative {
		bounds = append(bounds, ub)
	}
	sort.Float64s(bounds)

	for i, q := range qs {
		out[i] = quantileFromBuckets(bounds, cumulative, total, q)
	}
	// Pad to three return values for the common p50/p95/p99 call.
	for len(out) < 3 {
		out = append(out, 0)
	}
	return out[0], out[1], out[2]
}
