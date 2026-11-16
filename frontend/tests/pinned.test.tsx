import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "@heroui/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Bootstrap, ProbeResult, RunSummary } from "../src/types";

const stats={attempts:3,successes:3,successRate:1,averageMs:20,p50Ms:18,p95Ms:24,jitterMs:2,failures:{}};
const result:ProbeResult={candidate:{addressType:"ipv4",ip:"192.0.2.1",port:443,country:"US"},tcp:stats,https:stats,bandwidth:{bytes:1048576,ttfbMs:20,durationMs:100,mbps:80},score:90,parts:{tcp:90,https:90,jitter:90,reliability:100,bandwidth:90},status:"qualified"};
let data:Bootstrap;
let events:Record<string,(value:unknown)=>void>;
let api:NonNullable<NonNullable<NonNullable<Window["go"]>["main"]>["App"]>;

beforeEach(()=>{vi.resetModules();events={};delete window.go;delete window.runtime;});
afterEach(()=>{cleanup();delete window.go;delete window.runtime;vi.useRealTimers();vi.restoreAllMocks();});

async function nativeResults(failed=false){
  const {defaultSettings,defaultPublishSettings}=await import("../src/lib/bridge");
  const summary:RunSummary={runId:"ordinary",mode:"tcp_https",startedAt:new Date().toISOString(),finishedAt:new Date().toISOString(),state:"completed",results:[failed?{...result,pinned:true,status:"failed_gate",score:0}:result],failures:{},publications:[],scoreWeights:defaultSettings.scoreWeights,reusedStages:[]};
  data={settings:structuredClone(defaultSettings),publishSettings:structuredClone(defaultPublishSettings),pinnedNodes:[{mode:"h3",candidate:result.candidate}],sources:[],history:[summary],network:{interface:"test",ipv4:"",status:"online"},resume:{options:[]}};
  api={Bootstrap:vi.fn(async()=>data),SetNodePinned:vi.fn(async(_id,key,pinned)=>{
    const nodes=data.pinnedNodes!.filter(node=>node.mode!=="tcp_https");
    if(pinned)nodes.push({mode:"tcp_https",candidate:result.candidate});
    data.pinnedNodes=nodes;
    return nodes;
  })} as unknown as typeof api;
  window.go={main:{App:api}};
  window.runtime={EventsOn:(name,handler)=>{events[name]=handler;return ()=>{};}};
  const {actions}=await import("../src/store");
  await actions.init();
  const {ResultsTable}=await import("../src/features/results/ResultsTable");
  render(<ResultsTable summary={summary}/>);
  return {actions,summary};
}

describe("pinned result controls",()=>{
  it("saves and removes a pin independently of the other mode and reports failed saves",async()=>{
    await nativeResults();
    fireEvent.click(screen.getByRole("button",{name:"固化 192.0.2.1:443"}));
    await screen.findByRole("button",{name:"取消固化 192.0.2.1:443"});
    expect(api.SetNodePinned).toHaveBeenCalledExactlyOnceWith("ordinary","192.0.2.1:443",true);
    expect(data.pinnedNodes).toHaveLength(2);
    const danger=vi.spyOn(toast,"danger");
    vi.mocked(api.SetNodePinned).mockRejectedValueOnce(new Error("disk full"));
    fireEvent.click(screen.getByRole("button",{name:"取消固化 192.0.2.1:443"}));
    await waitFor(()=>expect(danger).toHaveBeenCalledWith(expect.stringContaining("disk full")));
    expect(screen.getByRole("button",{name:"取消固化 192.0.2.1:443"})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"取消固化 192.0.2.1:443"}));
    await screen.findByRole("button",{name:"固化 192.0.2.1:443"});
    expect(data.pinnedNodes).toEqual([{mode:"h3",candidate:result.candidate}]);
  });

  it("locks pin changes while a run is active",async()=>{
    const {actions}=await nativeResults();
    await act(async()=>events["run:progress"]({runId:"active",state:"running",startedAt:new Date().toISOString(),stages:[],failures:{}}));
    expect(screen.getByRole("button",{name:"固化 192.0.2.1:443"})).toBeDisabled();
    await expect(actions.setNodePinned("ordinary","192.0.2.1:443",true)).rejects.toThrow("测速运行中");
    expect(api.SetNodePinned).not.toHaveBeenCalled();
  });

  it("marks a failed pin as retained without presenting a qualifying score",async()=>{
    await nativeResults(true);
    expect(screen.getByText("固化保留 · 未通过")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
    // The historical pin snapshot does not turn on the current saved pin.
    expect(screen.getByRole("button",{name:"固化 192.0.2.1:443"})).toBeInTheDocument();
    fireEvent.click(screen.getByText("192.0.2.1:443"));
    await screen.findByText(/测速未通过，仍保留在最终列表；没有合格评分/);
    expect(screen.queryByText(/评分明细/)).not.toBeInTheDocument();
  });
});

it("keeps mock pins outside quotas and filters, isolates modes, and supports cancellation",async()=>{
  vi.useFakeTimers();
  const {bridge,defaultSettings}=await import("../src/lib/bridge");
  let summary:RunSummary|undefined;
  const off=bridge.onComplete(value=>{summary=value;});
  const run=async()=>{await bridge.startRun({startStage:"source",allowStaleCheckpoint:false});await vi.advanceTimersByTimeAsync(6000);return summary!;};
  await bridge.saveSettings({...defaultSettings,retainPreviousResults:false});
  const initial=await run();
  const node=initial.results[0].candidate;
  const key=`${node.ip}:${node.port}`;
  await bridge.setNodePinned(initial.runId,key,true);
  await bridge.saveSettings({...defaultSettings,retainPreviousResults:false,finalResultCount:1,allowedCountries:["US"],allowedPorts:[443]});
  const withPin=await run();
  expect(withPin.results).toHaveLength(2);
  expect(withPin.results.filter(result=>!result.pinned)).toHaveLength(1);
  expect(withPin.results.find(result=>result.pinned)?.candidate).toEqual(node);
  expect(withPin.results.filter(result=>result.candidate.ip===node.ip)).toHaveLength(1);
  await bridge.saveSettings({...defaultSettings,retainPreviousResults:false,finalResultCount:1,allowedPorts:[443]});
  expect(Object.values((await run()).failures).every(count=>count!>=0)).toBe(true);
  await bridge.saveSettings({...defaultSettings,requireH3:true,retainPreviousResults:false,finalResultCount:1});
  expect((await run()).results.some(result=>result.pinned)).toBe(false);
  await bridge.saveSettings({...defaultSettings,retainPreviousResults:false,finalResultCount:1});
  await bridge.startRun({startStage:"source",allowStaleCheckpoint:false});
  await expect(bridge.setNodePinned(withPin.runId,key,false)).rejects.toThrow("运行中");
  await bridge.cancelRun();
  expect(summary!.state).toBe("cancelled");
  expect((await bridge.bootstrap()).pinnedNodes).toHaveLength(1);
  for(let index=0;index<41;index++){
    await vi.advanceTimersByTimeAsync(1);
    await bridge.startRun({startStage:"source",allowStaleCheckpoint:false});
    await bridge.cancelRun();
  }
  const afterHistoryPruned=await run();
  expect(afterHistoryPruned.results.find(result=>result.pinned)?.status).toBe("failed_gate");
  await bridge.setNodePinned(afterHistoryPruned.runId,key,false);
  expect((await run()).results).toHaveLength(1);
  expect((await bridge.bootstrap()).pinnedNodes).toHaveLength(0);
  off();
});
