import type { Candidate, Mode, ProbeResult, ProbeStats } from "../types";
export const modeLabel = (mode?: Mode) => mode === "h3" ? "H3" : "普通（TCP/HTTPS）";
export const candidateAddress = (candidate: Candidate) => candidate.addressType === "domain" ? candidate.host ?? "" : candidate.ip;
const emptyStats: ProbeStats = {attempts:0,successes:0,successRate:0,averageMs:0,p50Ms:0,p95Ms:0,jitterMs:0,failures:{}};
export const connectionStats = (result: ProbeResult) => result.mode === "h3" ? result.quic ?? emptyStats : result.tcp;
export const httpStats = (result: ProbeResult) => result.mode === "h3" ? result.http3 ?? emptyStats : result.https;
