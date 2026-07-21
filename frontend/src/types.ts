export type FailureReason = "invalid_ip" | "invalid_port" | "invalid_tag" | "port_filtered" | "country_filtered" | "dns" | "tcp" | "tls" | "timeout" | "http_status" | "cancelled" | "download";

export interface Candidate { addressType: "ipv4"; ip: string; port: number; country?: string; sourceId?: string }
export interface ProbeStats { attempts: number; successes: number; successRate: number; averageMs: number; p50Ms: number; p95Ms: number; jitterMs: number; failures: Partial<Record<FailureReason, number>>; samplesMs?: number[] }
export interface BandwidthStats { bytes: number; ttfbMs: number; durationMs: number; mbps: number; failure?: FailureReason }
export interface ScoreParts { tcp: number; https: number; jitter: number; reliability: number; bandwidth: number }
export interface ProbeResult { candidate: Candidate; tcp: ProbeStats; https: ProbeStats; bandwidth: BandwidthStats; score: number; parts: ScoreParts; status: string }
export interface StageProgress { name: string; input: number; passed: number; failed: number; durationMs: number; state: string }
export interface RunProgress { runId: string; state: string; startedAt: string; stages: StageProgress[]; failures: Partial<Record<FailureReason, number>>; message?: string }
export interface RunSummary { runId: string; startedAt: string; finishedAt: string; state: string; results: ProbeResult[]; failures: Partial<Record<FailureReason, number>> }
export interface Settings { tcpConcurrency: number; httpsConcurrency: number; bandwidthConcurrency: number; connectTimeoutMs: number; requestTimeoutMs: number; bandwidthTimeoutMs: number; sourceTimeoutMs: number; sourceRetries: number; tcpProbeCount: number; httpsProbeCount: number; tcpMinSuccessRate: number; httpsMinSuccessRate: number; tcpCandidateCount: number; bandwidthCandidates: number; finalResultCount: number; maxDownloadBytes: number; allowedPorts: number[]; allowedCountries: string[]; blockedCountries: string[] }
export interface HTTPSource { id: string; name: string; url: string; enabled: boolean; lastFetched?: string; lastStatus?: string; nodeCount: number }
export interface NetworkInfo { interface: string; ipv4: string; status: string }
export interface Bootstrap { settings: Settings; sources: HTTPSource[]; history: RunSummary[]; network: NetworkInfo; currentRunId?: string }
