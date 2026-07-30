import { describe, expect, it } from "vitest";
import { formatCopyResults, type CopyFields } from "../src/features/results/ResultsTable";
import type { ProbeResult } from "../src/types";

const result: ProbeResult = {
  candidate: { addressType: "ipv4", ip: "104.18.1.20", port: 8443, country: "CN" },
  tcp: { attempts: 3, successes: 3, successRate: 1, averageMs: 18, p50Ms: 17, p95Ms: 22, jitterMs: 2, failures: {} },
  https: { attempts: 3, successes: 3, successRate: 1, averageMs: 44, p50Ms: 42, p95Ms: 48, jitterMs: 3, failures: {} },
  bandwidth: { bytes: 1048576, ttfbMs: 40, durationMs: 100, mbps: 186 },
  score: 90,
  parts: { tcp: 90, https: 88, jitter: 92, reliability: 100, bandwidth: 95 },
  status: "passed",
};

describe("formatCopyResults", () => {
  it("copies nodes and default metrics without tabs", () => {
    const fields: CopyFields = { country: true, tcpLatency: false, httpLatency: true, bandwidth: true };
    expect(formatCopyResults([result], fields)).toBe("104.18.1.20:8443#CN|HTTP44ms|186Mbps");
  });

  it("appends selected metrics with units", () => {
    const fields: CopyFields = { country: false, tcpLatency: true, httpLatency: true, bandwidth: true };
    expect(formatCopyResults([result], fields)).toBe("104.18.1.20:8443|TCP22ms|HTTP44ms|186Mbps");
  });
});
