export type FailureReason = "invalid_ip" | "invalid_port" | "invalid_tag" | "port_filtered" | "country_filtered" | "dns" | "tcp" | "tls" | "timeout" | "http_status" | "cancelled" | "download";

export interface Candidate { addressType: "ipv4"; ip: string; port: number; country?: string; sourceId?: string }
export interface ProbeStats { attempts: number; successes: number; successRate: number; averageMs: number; p50Ms: number; p95Ms: number; jitterMs: number; failures: Partial<Record<FailureReason, number>>; samplesMs?: number[] }
export interface BandwidthStats { bytes: number; ttfbMs: number; durationMs: number; mbps: number; failure?: FailureReason }
export interface ScoreParts { tcp: number; https: number; jitter: number; reliability: number; bandwidth: number }
export interface ProbeResult { candidate: Candidate; tcp: ProbeStats; https: ProbeStats; bandwidth: BandwidthStats; score: number; parts: ScoreParts; status: string }
export interface StageProgress { name: string; input: number; passed: number; failed: number; attemptsCompleted?: number; attemptsTotal?: number; durationMs: number; state: string }
export interface RunProgress { runId: string; state: string; startedAt: string; stages: StageProgress[]; failures: Partial<Record<FailureReason, number>>; message?: string }
export type PublishTarget = "cloudflare" | "github" | "gist" | "telegram";
export type PublishCredentialTarget = PublishTarget | "telegramRelay";
export type PublicationState = "queued" | "running" | "succeeded" | "failed" | "skipped";
export interface PublicationResult { target: PublishTarget; state: PublicationState; items: number; eligible?: number; skipped?: number; recordType?: "A" | "TXT"; url?: string; message?: string; startedAt?: string; finishedAt?: string }
export interface PublicationUpdate { runId: string; result: PublicationResult }
export interface RunSummary { runId: string; startedAt: string; finishedAt: string; state: string; results: ProbeResult[]; failures: Partial<Record<FailureReason, number>>; publications: PublicationResult[] }
export interface Settings { tcpConcurrency: number; httpsConcurrency: number; bandwidthConcurrency: number; connectTimeoutMs: number; requestTimeoutMs: number; bandwidthTimeoutMs: number; sourceTimeoutMs: number; sourceRetries: number; tcpProbeCount: number; httpsProbeCount: number; tcpMinSuccessRate: number; httpsMinSuccessRate: number; tcpCandidateCount: number; bandwidthCandidates: number; finalResultCount: number; maxDownloadBytes: number; allowedPorts: number[]; allowedCountries: string[]; blockedCountries: string[] }
export interface PublishOutputFields { country: boolean; tcpP95: boolean; httpLatency: boolean; bandwidth: boolean }
export interface PublishRequestPolicy { timeoutMs: number; maxRetries: number; retryDelayMs: number }
export interface CloudflarePublishView { enabled: boolean; tokenConfigured: boolean; zoneId: string; recordName: string; recordType: "A" | "TXT"; ttl: number; proxied: boolean }
export interface GitHubPublishView { enabled: boolean; tokenConfigured: boolean; owner: string; repository: string; branch: string; path: string }
export interface GistPublishView { enabled: boolean; tokenConfigured: boolean; gistId: string; filename: string }
export interface TelegramPublishView { enabled: boolean; tokenConfigured: boolean; chatId: string; contentMode: "summary" | "details"; deliveryMode: "direct" | "relay"; relayUrl: string; relayKeyConfigured: boolean }
export interface PublishSettingsView { output: PublishOutputFields; request: PublishRequestPolicy; cloudflare: CloudflarePublishView; github: GitHubPublishView; gist: GistPublishView; telegram: TelegramPublishView }
export interface PublishSaveRequest { settings: PublishSettingsView; cloudflareToken: string; githubToken: string; gistToken: string; telegramBotToken: string; telegramRelayKey: string }
export interface HTTPSource { id: string; name: string; url: string; enabled: boolean; lastFetched?: string; lastStatus?: string; nodeCount: number }
export interface NetworkInfo { interface: string; ipv4: string; status: string }
export interface Bootstrap { settings: Settings; publishSettings: PublishSettingsView; sources: HTTPSource[]; history: RunSummary[]; network: NetworkInfo; currentRunId?: string }
