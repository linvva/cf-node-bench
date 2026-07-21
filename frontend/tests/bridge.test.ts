import { describe, expect, it } from "vitest";
import { normalizeBootstrap, normalizeProgress, normalizeSummary } from "../src/lib/bridge";
import type { Bootstrap, RunProgress, RunSummary } from "../src/types";

describe("Wails bridge normalization",()=>{
  it("normalizes legacy null settings and history collections",()=>{
    const raw={settings:{probeCount:5,allowedPorts:null,allowedCountries:null,blockedCountries:null},sources:null,history:[{runId:"old",results:null,failures:null}],network:null} as unknown as Bootstrap;
    const value=normalizeBootstrap(raw);
    expect(value.settings.tcpProbeCount).toBe(5);
    expect(value.settings.httpsProbeCount).toBe(5);
    expect(value.settings.allowedPorts).toEqual([]);
    expect(value.settings.allowedCountries).toEqual([]);
    expect(value.settings.blockedCountries).toEqual([]);
    expect(value.sources).toEqual([]);
    expect(value.history[0].results).toEqual([]);
    expect(value.history[0].failures).toEqual({});
  });

  it("normalizes cancelled summaries and initial progress",()=>{
    const summary=normalizeSummary({results:null,failures:null} as unknown as RunSummary);
    const progress=normalizeProgress({stages:null,failures:null} as unknown as RunProgress);
    expect(summary.results).toEqual([]);
    expect(progress.stages).toEqual([]);
  });
});
