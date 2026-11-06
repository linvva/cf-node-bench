import { afterEach, describe, expect, it, vi } from "vitest";
import { bridge, defaultSettings, defaultPublishSettings, DEFAULT_VERIFICATION_HOST, effectiveVerificationHost, normalizeBootstrap } from "../src/lib/bridge";
import { formatCopyResults, formatCSVResults } from "../src/features/results/ResultsTable";
import { validateSettings } from "../src/features/settings/SettingsPage";
import { connectionWeight,httpWeight } from "../src/lib/scoring";
import type { Bootstrap, ProbeResult, PublicationResult, RunSummary } from "../src/types";

const stats={attempts:3,successes:3,successRate:1,averageMs:44,p50Ms:42,p95Ms:51,jitterMs:3,failures:{}};
const result:ProbeResult={mode:"h3",candidate:{addressType:"domain",host:"edge.example.com",ip:"",port:8443},tcp:{...stats,successRate:0},https:{...stats,successRate:0},quic:stats,http3:stats,bandwidth:{bytes:1048576,ttfbMs:40,durationMs:100,mbps:80},score:90,parts:{tcp:0,https:0,quic:90,http3:85,jitter:90,reliability:100,bandwidth:80},status:"qualified",resolvedAt:"2026-01-01T00:00:00Z",endpoints:[{ip:"192.0.2.10",quic:stats,http3:stats,bandwidth:{bytes:1048576,ttfbMs:40,durationMs:100,mbps:80}}]};
afterEach(()=>vi.useRealTimers());
describe("unified H3 contract",()=>{
  it("uses a public verification domain and permits an empty override",()=>{
    expect(defaultSettings.requireH3).toBe(false);
    expect(defaultSettings.verificationHost).toBe("");expect(effectiveVerificationHost(defaultSettings)).toBe(DEFAULT_VERIFICATION_HOST);
    expect(defaultSettings.h3BandwidthTestUrl).toContain("https://h3.speed.cloudflare.com/");
    expect(validateSettings({...defaultSettings,requireH3:true,verificationHost:""})).toBe("");
    expect(validateSettings({...defaultSettings,verificationHost:"worker.example.com"})).toBe("");
    expect(validateSettings({...defaultSettings,verificationHost:"https://worker.example.com/path"})).toContain("验证域名");
    expect(validateSettings({...defaultSettings,connectionConcurrency:0})).toContain("连接并发");
    expect(validateSettings({...defaultSettings,httpMinSuccessRate:0.5})).toContain("最低成功率");
    expect(validateSettings({...defaultSettings,httpCandidateCount:10})).toContain("HTTP 候选数");
  });
  it("copies original domains with genuine H3 metrics and exports the IPv4 snapshot",()=>{
    expect(formatCopyResults([result],{country:false,tcpLatency:true,httpLatency:true,bandwidth:true})).toBe("edge.example.com:8443#H3|QUIC51ms|HTTP344ms|80Mbps");
    const csv=formatCSVResults([result],"h3");
    expect(csv).toContain("QUIC Success");expect(csv).toContain("edge.example.com");expect(csv).toContain("192.0.2.10");expect(csv).toContain(result.resolvedAt);expect(csv).not.toContain("TCP");
    expect(connectionWeight({tcp:20,https:25,bandwidth:40,jitter:5,reliability:10})).toBe(20);
    expect(httpWeight({quic:15,http3:25,bandwidth:40,jitter:5,reliability:15},"h3")).toBe(25);
    expect(connectionWeight(defaultSettings.scoreWeights,"h3")).toBe(15);
  });
  it("shares settings, publication and history while invalidating incompatible checkpoints",async()=>{
    vi.useFakeTimers();
    await bridge.saveSettings({...defaultSettings,requireH3:true,verificationHost:"",connectTimeoutMs:1400});
    await bridge.savePublishSettings({settings:{...defaultPublishSettings,cloudflare:{...defaultPublishSettings.cloudflare,enabled:true,zoneId:"shared",h3:{recordName:"h3.example.com",recordType:"A"}}},cloudflareToken:"test-value",githubToken:"",gistToken:"test-value",telegramBotToken:"",telegramRelayKey:""});
    let summary:RunSummary|undefined;
    const off=bridge.onComplete(value=>{summary=value;});
    await bridge.startRun({startStage:"source",allowStaleCheckpoint:false});
    await expect(bridge.saveSettings(defaultSettings)).rejects.toThrow("测速中");
    await vi.advanceTimersByTimeAsync(6000);
    expect(summary?.mode).toBe("h3");expect(summary?.workerHost).toBe(DEFAULT_VERIFICATION_HOST);expect(summary?.results[0].candidate.host).toBe("edge.example.com");expect(summary?.results[0].endpoints).toHaveLength(2);
    const h3Id=summary!.runId;
    let publication:PublicationResult|undefined;
    const offPublication=bridge.onPublish(update=>{publication=update.result;});
    await bridge.publishRun(h3Id,"cloudflare");
    const saved=await bridge.bootstrap();
    await bridge.savePublishSettings({settings:{...saved.publishSettings,cloudflare:{...saved.publishSettings.cloudflare,recordType:"TXT"}},cloudflareToken:"",githubToken:"",gistToken:"",telegramBotToken:"",telegramRelayKey:""});
    await vi.advanceTimersByTimeAsync(200);
    expect(publication?.recordType).toBe("A");expect(publication?.skipped).toBeGreaterThan(0);offPublication();
    await bridge.saveSettings({...saved.settings,requireH3:false});
    const ordinary=await bridge.bootstrap();
    expect(ordinary.settings.connectTimeoutMs).toBe(1400);expect(ordinary.publishSettings.gist.tokenConfigured).toBe(true);expect(ordinary.publishSettings.cloudflare.recordType).toBe("TXT");expect(ordinary.history[0].runId).toBe(h3Id);
    expect(ordinary.resume.options.filter(option=>option.available).map(option=>option.stage)).toEqual(["tcp"]);
    await expect(bridge.startRun({startStage:"bandwidth",baseRunId:h3Id,allowStaleCheckpoint:true})).rejects.toThrow("检查点");
    await bridge.startRun({startStage:"tcp",baseRunId:h3Id,allowStaleCheckpoint:false});await vi.advanceTimersByTimeAsync(6000);
    expect(summary?.mode).toBe("tcp_https");expect((await bridge.bootstrap()).history.map(item=>item.mode)).toContain("h3");
    await bridge.saveSettings({...saved.settings,requireH3:true,verificationHost:"other.example.com"});
    const changed=await bridge.bootstrap();expect(changed.resume.options.filter(option=>option.available).map(option=>option.stage)).toEqual(["quic"]);
    off();await bridge.saveSettings(defaultSettings);
  });
  it("keeps a single history of at most forty entries and surfaces migration notices",()=>{
    const history=Array.from({length:44},(_,i)=>({runId:String(i),mode:i%2?"h3":"tcp_https",results:[]} as unknown as RunSummary));
    const normalized=normalizeBootstrap({settings:defaultSettings,history,sources:[],migrationNotice:"已合并设置"} as unknown as Bootstrap);
    expect(normalized.history).toHaveLength(40);expect(normalized.history[0].mode).toBe("tcp_https");expect(normalized.history[1].mode).toBe("h3");expect(normalized.migrationNotice).toBe("已合并设置");
  });
});
