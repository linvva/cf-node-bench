package model

import "time"

type AddressType string

const AddressIPv4 AddressType = "ipv4"

type Candidate struct {
	AddressType AddressType `json:"addressType"`
	IP          string      `json:"ip"`
	Port        int         `json:"port"`
	Country     string      `json:"country,omitempty"`
	SourceID    string      `json:"sourceId,omitempty"`
}

func (c Candidate) Key() string { return c.IP + ":" + itoa(c.Port) }

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}

type FailureReason string

const (
	FailureInvalidIP       FailureReason = "invalid_ip"
	FailureInvalidPort     FailureReason = "invalid_port"
	FailureInvalidTag      FailureReason = "invalid_tag"
	FailurePortFiltered    FailureReason = "port_filtered"
	FailureCountryFiltered FailureReason = "country_filtered"
	FailureDNS             FailureReason = "dns"
	FailureTCP             FailureReason = "tcp"
	FailureTLS             FailureReason = "tls"
	FailureTimeout         FailureReason = "timeout"
	FailureHTTPStatus      FailureReason = "http_status"
	FailureCancelled       FailureReason = "cancelled"
	FailureDownload        FailureReason = "download"
)

type ProbeStats struct {
	Attempts    int                   `json:"attempts"`
	Successes   int                   `json:"successes"`
	SuccessRate float64               `json:"successRate"`
	AverageMS   float64               `json:"averageMs"`
	P50MS       float64               `json:"p50Ms"`
	P95MS       float64               `json:"p95Ms"`
	JitterMS    float64               `json:"jitterMs"`
	Failures    map[FailureReason]int `json:"failures"`
	SamplesMS   []float64             `json:"samplesMs,omitempty"`
}

type BandwidthStats struct {
	Bytes      int64         `json:"bytes"`
	TTFBMS     float64       `json:"ttfbMs"`
	DurationMS float64       `json:"durationMs"`
	Mbps       float64       `json:"mbps"`
	Failure    FailureReason `json:"failure,omitempty"`
}

type ScoreParts struct {
	TCP         float64 `json:"tcp"`
	HTTPS       float64 `json:"https"`
	Jitter      float64 `json:"jitter"`
	Reliability float64 `json:"reliability"`
	Bandwidth   float64 `json:"bandwidth"`
}

type ProbeResult struct {
	Candidate Candidate      `json:"candidate"`
	TCP       ProbeStats     `json:"tcp"`
	HTTPS     ProbeStats     `json:"https"`
	Bandwidth BandwidthStats `json:"bandwidth"`
	Score     float64        `json:"score"`
	Parts     ScoreParts     `json:"parts"`
	Status    string         `json:"status"`
}

type StageProgress struct {
	Name       string `json:"name"`
	Input      int    `json:"input"`
	Passed     int    `json:"passed"`
	Failed     int    `json:"failed"`
	DurationMS int64  `json:"durationMs"`
	State      string `json:"state"`
}

type RunProgress struct {
	RunID     string                `json:"runId"`
	State     string                `json:"state"`
	StartedAt time.Time             `json:"startedAt"`
	Stages    []StageProgress       `json:"stages"`
	Failures  map[FailureReason]int `json:"failures"`
	Message   string                `json:"message,omitempty"`
}

type RunSummary struct {
	RunID      string                `json:"runId"`
	StartedAt  time.Time             `json:"startedAt"`
	FinishedAt time.Time             `json:"finishedAt"`
	State      string                `json:"state"`
	Results    []ProbeResult         `json:"results"`
	Failures   map[FailureReason]int `json:"failures"`
}
