export type Mode = "tcp_https" | "h3";
export type FailureReason = "quic" | "quic_timeout" | "protocol" | "invalid_ip" | "invalid_port" | "invalid_tag" | "port_filtered" | "country_filtered" | "dns" | "tcp" | "tls" | "timeout" | "http_status" | "cancelled" | "download";

export interface Candidate { addressType: "ipv4" | "domain"; ip: string; host?: string; port: number; country?: string; sourceId?: string }
export interface ProbeStats { attempts: number; successes: number; successRate: number; averageMs: number; p50Ms: number; p95Ms: number; jitterMs: number; failures: Partial<Record<FailureReason, number>>; samplesMs?: number[] }
export interface BandwidthStats { bytes: number; ttfbMs: number; durationMs: number; mbps: number; failure?: FailureReason }
export interface ScoreParts { tcp: number; https: number; quic?: number; http3?: number; jitter: number; reliability: number; bandwidth: number }
export interface ScoreWeights { connection?: number; http?: number; tcp?: number; https?: number; quic?: number; http3?: number; jitter: number; reliability: number; bandwidth: number }
export interface EndpointResult { ip: string; quic: ProbeStats; http3: ProbeStats; bandwidth: BandwidthStats }
export interface ProbeResult { mode?: Mode; quic?: ProbeStats; http3?: ProbeStats; resolvedAt?: string; endpoints?: EndpointResult[]; candidate: Candidate; tcp: ProbeStats; https: ProbeStats; bandwidth: BandwidthStats; score: number; parts: ScoreParts; status: string }
export type StageState = "pending" | "running" | "completed" | "reused" | "skipped" | "cancelled";
export interface StageProgress { name: string; input: number; passed: number; failed: number; attemptsCompleted?: number; attemptsTotal?: number; durationMs: number; state: StageState; measuredAt?: string }
export interface RunProgress { mode?: Mode; logicalCandidates?: number; physicalCandidates?: number; downloadedBytes?: number; runId: string; state: string; startedAt: string; stages: StageProgress[]; failures: Partial<Record<FailureReason, number>>; message?: string; checkpointWarning?: string }
export type PublishTarget = "cloudflare" | "github" | "gist" | "telegram";
export type PublishCredentialTarget = PublishTarget | "telegramRelay";
export type PublicationState = "queued" | "running" | "succeeded" | "failed" | "skipped";
export interface PublicationResult { target: PublishTarget; state: PublicationState; items: number; eligible?: number; skipped?: number; recordType?: "A" | "TXT"; url?: string; message?: string; startedAt?: string; finishedAt?: string }
export interface PublicationUpdate { runId: string; result: PublicationResult }
export type RunStartStage = "source" | "tcp" | "https" | "quic" | "http3" | "bandwidth" | "ranking";
export interface RunStartRequest { startStage: RunStartStage; baseRunId?: string; allowStaleCheckpoint: boolean }
export interface ResumeOption { stage: Exclude<RunStartStage,"source">; available: boolean; input: number; measuredAt?: string; warnings: string[] }
export interface ResumeInfo { baseRunId?: string; updatedAt?: string; options: ResumeOption[]; error?: string }
export interface RunSummary { mode?: Mode; workerHost?: string; logicalCandidates?: number; physicalCandidates?: number; downloadedBytes?: number; runId: string; startedAt: string; finishedAt: string; state: string; results: ProbeResult[]; failures: Partial<Record<FailureReason, number>>; publications: PublicationResult[]; scoreWeights: ScoreWeights; parentRunId?: string; startStage?: RunStartStage; reusedStages: string[]; checkpointWarning?: string }
export interface Settings { requireH3: boolean; verificationHost: string; h3ConnectTimeoutMs: number; h3RequestTimeoutMs: number; h3BandwidthTestUrl: string; connectionConcurrency: number; httpConcurrency: number; connectionProbeCount: number; httpProbeCount: number; connectionMinSuccessRate: number; httpMinSuccessRate: number; httpCandidateCount: number; bandwidthConcurrency: number; connectTimeoutMs: number; requestTimeoutMs: number; bandwidthTimeoutMs: number; sourceTimeoutMs: number; sourceRetries: number; bandwidthCandidates: number; finalResultCount: number; maxDownloadBytes: number; bandwidthTestUrl: string; retainPreviousResults: boolean; scoreWeights: ScoreWeights; allowedPorts: number[]; allowedCountries: string[]; blockedCountries: string[] }
export interface PublishOutputFields { country: boolean; tcpP95: boolean; httpLatency: boolean; bandwidth: boolean }
export interface PublishRequestPolicy { timeoutMs: number; maxRetries: number; retryDelayMs: number }
export interface CloudflareH3Target { recordName: string; recordType: "A" | "TXT" }
export interface GitHubH3Target { path: string }
export interface GistH3Target { gistId: string; filename: string }
export interface CloudflarePublishView { h3: CloudflareH3Target; enabled: boolean; tokenConfigured: boolean; zoneId: string; recordName: string; recordType: "A" | "TXT"; ttl: number; proxied: boolean }
export interface GitHubPublishView { h3: GitHubH3Target; enabled: boolean; tokenConfigured: boolean; owner: string; repository: string; branch: string; path: string }
export interface GistPublishView { h3: GistH3Target; enabled: boolean; tokenConfigured: boolean; gistId: string; filename: string }
export interface TelegramPublishView { enabled: boolean; tokenConfigured: boolean; chatId: string; contentMode: "summary" | "details"; deliveryMode: "direct" | "relay"; relayUrl: string; relayKeyConfigured: boolean }
export interface PublishSettingsView { output: PublishOutputFields; request: PublishRequestPolicy; cloudflare: CloudflarePublishView; github: GitHubPublishView; gist: GistPublishView; telegram: TelegramPublishView }
export interface PublishSaveRequest { testMode?: Mode; settings: PublishSettingsView; cloudflareToken: string; githubToken: string; gistToken: string; telegramBotToken: string; telegramRelayKey: string }
export interface HTTPSource { id: string; name: string; url: string; enabled: boolean; lastFetched?: string; lastStatus?: string; nodeCount: number }
export interface NetworkInfo { interface: string; ipv4: string; status: string }
export interface Bootstrap { migrationNotice?: string; settings: Settings; publishSettings: PublishSettingsView; sources: HTTPSource[]; history: RunSummary[]; network: NetworkInfo; currentRunId?: string; currentResultRunId?: string; resume: ResumeInfo }
