package model

import "time"

type AddressType string

const AddressIPv4 AddressType = "ipv4"
const AddressDomain AddressType = "domain"

type Mode string

const (
	ModeTCPHTTPS Mode = "tcp_https"
	ModeH3       Mode = "h3"
)

func NormalizeMode(mode Mode) Mode {
	if mode == "" {
		return ModeTCPHTTPS
	}
	return mode
}

type Candidate struct {
	AddressType AddressType `json:"addressType"`
	IP          string      `json:"ip"`
	Host        string      `json:"host,omitempty"`
	Port        int         `json:"port"`
	Country     string      `json:"country,omitempty"`
	SourceID    string      `json:"sourceId,omitempty"`
}

func (c Candidate) Address() string {
	if c.AddressType == AddressDomain {
		return c.Host
	}
	return c.IP
}

func (c Candidate) Key() string { return c.Address() + ":" + itoa(c.Port) }

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
	FailureInvalidIP            FailureReason = "invalid_ip"
	FailureInvalidPort          FailureReason = "invalid_port"
	FailureInvalidTag           FailureReason = "invalid_tag"
	FailurePortFiltered         FailureReason = "port_filtered"
	FailureCountryFiltered      FailureReason = "country_filtered"
	FailureDNS                  FailureReason = "dns"
	FailureTCP                  FailureReason = "tcp"
	FailureTLS                  FailureReason = "tls"
	FailureTimeout              FailureReason = "timeout"
	FailureHTTPStatus           FailureReason = "http_status"
	FailureCancelled            FailureReason = "cancelled"
	FailureDownload             FailureReason = "download"
	FailureQUIC                 FailureReason = "quic"
	FailureQUICHandshakeTimeout FailureReason = "quic_timeout"
	FailureProtocol             FailureReason = "protocol"
)

type ProbeStats struct {
	Attempts    int                   `json:"attempts"`
	Skipped     int                   `json:"skipped,omitempty"`
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
	QUIC        float64 `json:"quic,omitempty"`
	HTTP3       float64 `json:"http3,omitempty"`
	TCP         float64 `json:"tcp"`
	HTTPS       float64 `json:"https"`
	Jitter      float64 `json:"jitter"`
	Reliability float64 `json:"reliability"`
	Bandwidth   float64 `json:"bandwidth"`
}

type ScoreWeights struct {
	Connection  int `json:"connection,omitempty"`
	HTTP        int `json:"http,omitempty"`
	QUIC        int `json:"quic,omitempty"`
	HTTP3       int `json:"http3,omitempty"`
	TCP         int `json:"tcp"`
	HTTPS       int `json:"https"`
	Jitter      int `json:"jitter"`
	Reliability int `json:"reliability"`
	Bandwidth   int `json:"bandwidth"`
}

type ProbeResult struct {
	Mode       Mode             `json:"mode,omitempty"`
	QUIC       ProbeStats       `json:"quic"`
	HTTP3      ProbeStats       `json:"http3"`
	ResolvedAt time.Time        `json:"resolvedAt,omitempty"`
	Endpoints  []EndpointResult `json:"endpoints,omitempty"`
	Candidate  Candidate        `json:"candidate"`
	TCP        ProbeStats       `json:"tcp"`
	HTTPS      ProbeStats       `json:"https"`
	Bandwidth  BandwidthStats   `json:"bandwidth"`
	Score      float64          `json:"score"`
	Parts      ScoreParts       `json:"parts"`
	Status     string           `json:"status"`
}

type EndpointResult struct {
	IP        string         `json:"ip"`
	QUIC      ProbeStats     `json:"quic"`
	HTTP3     ProbeStats     `json:"http3"`
	Bandwidth BandwidthStats `json:"bandwidth"`
}

type StageProgress struct {
	Name              string    `json:"name"`
	Input             int       `json:"input"`
	Passed            int       `json:"passed"`
	Failed            int       `json:"failed"`
	AttemptsCompleted int       `json:"attemptsCompleted,omitempty"`
	AttemptsSkipped   int       `json:"attemptsSkipped,omitempty"`
	AttemptsTotal     int       `json:"attemptsTotal,omitempty"`
	DurationMS        int64     `json:"durationMs"`
	State             string    `json:"state"`
	MeasuredAt        time.Time `json:"measuredAt,omitempty"`
}

type RunProgress struct {
	LocalFiles         bool                  `json:"localFiles,omitempty"`
	Mode               Mode                  `json:"mode,omitempty"`
	LogicalCandidates  int                   `json:"logicalCandidates,omitempty"`
	PhysicalCandidates int                   `json:"physicalCandidates,omitempty"`
	DownloadedBytes    int64                 `json:"downloadedBytes,omitempty"`
	RunID              string                `json:"runId"`
	State              string                `json:"state"`
	StartedAt          time.Time             `json:"startedAt"`
	Stages             []StageProgress       `json:"stages"`
	Failures           map[FailureReason]int `json:"failures"`
	Message            string                `json:"message,omitempty"`
	CheckpointWarning  string                `json:"checkpointWarning,omitempty"`
}

type RunSummary struct {
	LocalFiles         bool                  `json:"localFiles,omitempty"`
	Mode               Mode                  `json:"mode,omitempty"`
	WorkerHost         string                `json:"workerHost,omitempty"`
	LogicalCandidates  int                   `json:"logicalCandidates,omitempty"`
	PhysicalCandidates int                   `json:"physicalCandidates,omitempty"`
	DownloadedBytes    int64                 `json:"downloadedBytes,omitempty"`
	RunID              string                `json:"runId"`
	StartedAt          time.Time             `json:"startedAt"`
	FinishedAt         time.Time             `json:"finishedAt"`
	State              string                `json:"state"`
	Results            []ProbeResult         `json:"results"`
	Failures           map[FailureReason]int `json:"failures"`
	Publications       []PublicationResult   `json:"publications"`
	ScoreWeights       ScoreWeights          `json:"scoreWeights"`
	ParentRunID        string                `json:"parentRunId,omitempty"`
	StartStage         string                `json:"startStage,omitempty"`
	ReusedStages       []string              `json:"reusedStages,omitempty"`
	CheckpointWarning  string                `json:"checkpointWarning,omitempty"`
}

type PublicationResult struct {
	Target     string    `json:"target"`
	State      string    `json:"state"`
	Items      int       `json:"items"`
	Eligible   int       `json:"eligible,omitempty"`
	Skipped    int       `json:"skipped,omitempty"`
	RecordType string    `json:"recordType,omitempty"`
	URL        string    `json:"url,omitempty"`
	Message    string    `json:"message,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
}

// ForMode projects shared weights onto the measured protocol; legacy history stays readable.
func (w ScoreWeights) ForMode(mode Mode) ScoreWeights {
	if w.Connection != 0 || w.HTTP != 0 {
		w.TCP, w.HTTPS, w.QUIC, w.HTTP3 = 0, 0, 0, 0
		if mode == ModeH3 {
			w.QUIC, w.HTTP3 = w.Connection, w.HTTP
		} else {
			w.TCP, w.HTTPS = w.Connection, w.HTTP
		}
	}
	return w
}
