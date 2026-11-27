import { clearNative, mockNative, type NativeApp } from "./native";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { toast } from "@heroui/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Bootstrap, ProbeResult, RunProgress, RunSummary } from "../src/types";

const stats={attempts:3,successes:3,successRate:1,averageMs:20,p50Ms:18,p95Ms:24,jitterMs:2,failures:{}};
const result:ProbeResult={candidate:{addressType:"ipv4",ip:"192.0.2.1",port:443,country:"US"},tcp:stats,https:stats,bandwidth:{bytes:1048576,ttfbMs:20,durationMs:100,mbps:80},score:90,parts:{tcp:90,https:90,jitter:90,reliability:100,bandwidth:90},status:"qualified"};
let data:Bootstrap;
let events:Record<string,(value:unknown)=>void>;
let api:NativeApp;

beforeEach(()=>{vi.resetModules();events={};clearNative();});
afterEach(()=>{cleanup();clearNative();vi.useRealTimers();vi.restoreAllMocks();});

async function workspace(availableResults=2){
  const {defaultSettings,defaultPublishSettings}=await import("../src/lib/bridge");
  const previous:RunSummary={runId:"previous",mode:"tcp_https",startedAt:new Date().toISOString(),finishedAt:new Date().toISOString(),state:"completed",results:[result],failures:{},publications:[],scoreWeights:defaultSettings.scoreWeights,reusedStages:[]};
  data={settings:structuredClone(defaultSettings),publishSettings:structuredClone(defaultPublishSettings),sources:[],history:[previous],network:{interface:"test",ipv4:"",status:"online"},resume:{options:[]}};
  api={Bootstrap:vi.fn(async()=>data),StartRun:vi.fn(async()=>"next"),CancelRun:vi.fn(async()=>true),CancelRunWithResults:vi.fn(async()=>true),PublishRun:vi.fn(async()=>{})} as unknown as typeof api;
  mockNative(api,events);
  const {actions}=await import("../src/store");
  await actions.init();
  const {RunWorkspace}=await import("../src/features/run/RunWorkspace");
  render(<RunWorkspace/>);
  const update=async(count=availableResults,runId="active")=>act(async()=>events["run:progress"]({runId,state:"running",availableResults:count,startedAt:previous.startedAt,stages:[{name:"bandwidth",input:20,passed:9,failed:0,durationMs:100,state:"running"}],failures:{}} satisfies RunProgress));
  const finish=async(partialResults=false)=>{
    const summary={...previous,runId:"active",state:partialResults?"cancelled":"completed",partialResults,results:[result]};
    data.history=[summary,previous];
    await act(async()=>events["run:complete"](summary));
  };
  await update();
  return {actions,update,finish};
}

describe("cancelling with complete results",()=>{
  it("uses full-result availability instead of a stage's passed count",async()=>{
    await workspace(0);
    fireEvent.click(screen.getByRole("button",{name:"取消测速"}));
    await waitFor(()=>expect(api.CancelRun).toHaveBeenCalledOnce());
    expect(api.CancelRunWithResults).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("keeps complete results, shows the actual count, and allows manual publishing and copying",async()=>{
    const {update,finish}=await workspace();
    fireEvent.click(screen.getByRole("button",{name:"取消测速"}));
    const dialog=await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("当前有 2 个节点");
    expect(dialog).toHaveTextContent("最终结果数选优");
    expect(api.CancelRun).not.toHaveBeenCalled();
    await update(3);
    expect(dialog).toHaveTextContent("当前有 3 个节点");
    fireEvent.click(within(dialog).getByRole("button",{name:"保留已通过节点并结束"}));
    await waitFor(()=>expect(api.CancelRunWithResults).toHaveBeenCalledExactlyOnceWith(true));
    await finish(true);
    expect(screen.getByText("已取消，已保留部分结果 · 1 个节点")).toBeInTheDocument();
    expect(screen.getByRole("option",{name:/已取消，已保留部分结果/})).toBeInTheDocument();
    expect(api.PublishRun).not.toHaveBeenCalled();
    const clipboard=vi.spyOn(navigator.clipboard,"writeText");
    fireEvent.click(screen.getByRole("button",{name:"复制全部 1 个"}));
    await waitFor(()=>expect(clipboard).toHaveBeenCalledWith(expect.stringContaining("192.0.2.1:443")));
    expect(screen.getByRole("button",{name:"导出 CSV"})).toBeEnabled();
    fireEvent.click(screen.getByRole("button",{name:"发布结果"}));
    await waitFor(()=>expect(api.PublishRun).toHaveBeenCalledExactlyOnceWith("active","all"));
  });

  it.each([false,true])("publishes the latest retained partial result from the publish page (only history=%s)",async(onlyHistory)=>{
    const {actions,finish}=await workspace();
    await finish(true);
    if(onlyHistory){data.history=data.history.slice(0,1);await act(async()=>actions.init());}
    cleanup();
    const {PublishPage}=await import("../src/features/publish/PublishPage");
    render(<PublishPage/>);
    const publish=screen.getByRole("button",{name:"发布最近结果"});
    expect(publish).toBeEnabled();
    expect(api.PublishRun).not.toHaveBeenCalled();
    fireEvent.click(publish);
    await waitFor(()=>expect(api.PublishRun).toHaveBeenCalledExactlyOnceWith("active","all"));
  });

  it("continues when dismissed and uses the legacy discard cancellation when chosen",async()=>{
    await workspace();
    const open=()=>fireEvent.click(screen.getByRole("button",{name:"取消测速"}));
    open();
    fireEvent.click(await screen.findByRole("button",{name:"继续测速并关闭"}));
    await waitFor(()=>expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    open();
    fireEvent.click(await screen.findByRole("button",{name:"继续测速"}));
    await waitFor(()=>expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(api.CancelRun).not.toHaveBeenCalled();
    expect(api.CancelRunWithResults).not.toHaveBeenCalled();
    open();
    fireEvent.click(await screen.findByRole("button",{name:"丢弃本轮结果并取消"}));
    await waitFor(()=>expect(api.CancelRun).toHaveBeenCalledOnce());
    expect(api.CancelRunWithResults).not.toHaveBeenCalled();
  });

  it("shows cancellation errors and locks repeated choices while waiting",async()=>{
    const {actions,finish}=await workspace();
    const danger=vi.spyOn(toast,"danger");
    vi.mocked(api.CancelRunWithResults).mockRejectedValueOnce(new Error("disk full"));
    fireEvent.click(screen.getByRole("button",{name:"取消测速"}));
    fireEvent.click(await screen.findByRole("button",{name:"保留已通过节点并结束"}));
    await waitFor(()=>expect(danger).toHaveBeenCalledWith(expect.stringContaining("disk full")));
    let resolve!:(value:boolean)=>void;
    vi.mocked(api.CancelRunWithResults).mockImplementationOnce(()=>new Promise(value=>{resolve=value;}));
    fireEvent.click(screen.getByRole("button",{name:"保留已通过节点并结束"}));
    await waitFor(()=>expect(screen.getByRole("button",{name:"保留已通过节点并结束"})).toBeDisabled());
    expect(screen.getByRole("button",{name:"丢弃本轮结果并取消"})).toBeDisabled();
    expect(screen.getByRole("button",{name:"继续测速"})).toBeDisabled();
    fireEvent.click(screen.getByRole("button",{name:"保留已通过节点并结束"}));
    expect(api.CancelRunWithResults).toHaveBeenCalledTimes(2);
    await act(async()=>resolve(true));
    await waitFor(()=>expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByRole("button",{name:"取消测速"})).toBeDisabled();
    await expect(actions.cancel(false,"active")).resolves.toBe(false);
    expect(api.CancelRun).not.toHaveBeenCalled();
    await finish(true);
    expect(screen.getByRole("button",{name:"开始测速"})).toBeEnabled();
  });

  it("closes on natural completion and ignores a stale choice for the next run",async()=>{
    const {actions,finish}=await workspace();
    fireEvent.click(screen.getByRole("button",{name:"取消测速"}));
    await screen.findByRole("dialog");
    await finish();
    await waitFor(()=>expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    fireEvent.click(screen.getByRole("button",{name:"开始测速"}));
    await waitFor(()=>expect(api.StartRun).toHaveBeenCalledOnce());
    await expect(actions.cancel(true,"active")).resolves.toBe(false);
    expect(api.CancelRunWithResults).not.toHaveBeenCalled();
    expect(api.CancelRun).not.toHaveBeenCalled();
    expect(screen.getByRole("button",{name:"取消测速"})).toBeEnabled();
  });

  it("waits for an outstanding cancellation RPC before allowing the next run",async()=>{
    const {finish}=await workspace();
    let resolve!:(value:boolean)=>void;
    vi.mocked(api.CancelRunWithResults).mockImplementationOnce(()=>new Promise(value=>{resolve=value;}));
    fireEvent.click(screen.getByRole("button",{name:"取消测速"}));
    fireEvent.click(await screen.findByRole("button",{name:"保留已通过节点并结束"}));
    await finish();
    await waitFor(()=>expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByRole("button",{name:"开始测速"})).toBeDisabled();
    await act(async()=>resolve(false));
    expect(screen.getByRole("button",{name:"开始测速"})).toBeEnabled();
    fireEvent.click(screen.getByRole("button",{name:"开始测速"}));
    await waitFor(()=>expect(api.StartRun).toHaveBeenCalledOnce());
  });
});

it.each([false,true])("mock keeps completed nodes without auto-publishing and supports explicit/default discard (H3=%s)",async(requireH3)=>{
  vi.useFakeTimers();
  const {bridge,defaultSettings,defaultPublishSettings}=await import("../src/lib/bridge");
  await bridge.saveSettings({...defaultSettings,requireH3,retainPreviousResults:false,finalResultCount:3});
  await bridge.savePublishSettings({settings:{...defaultPublishSettings,github:{...defaultPublishSettings.github,enabled:true,path:"ip.txt",h3:{path:"h3.txt"}}},cloudflareToken:"",githubToken:"token",gistToken:"",telegramBotToken:"",telegramRelayKey:""});
  let progress:RunProgress|undefined,summary:RunSummary|undefined;
  const publish=vi.fn();
  const offProgress=bridge.onProgress(value=>{progress=value;});
  const offComplete=bridge.onComplete(value=>{summary=value;});
  const offPublish=bridge.onPublish(publish);
  const request={startStage:"source" as const,allowStaleCheckpoint:false};
  await bridge.startRun(request);
  await vi.advanceTimersByTimeAsync(2600);
  expect(progress!.availableResults).toBe(0);
  await vi.advanceTimersByTimeAsync(520);
  expect(progress!.availableResults).toBeGreaterThan(3);
  expect(await bridge.cancelRun(true)).toBe(true);
  expect(summary!.state).toBe("cancelled");
  expect(summary!.partialResults).toBe(true);
  expect(summary!.results).toHaveLength(3);
  expect(summary!.results.every(result=>result.status==="qualified"&&result.bandwidth.bytes>0)).toBe(true);
  if(requireH3)expect(summary!.results.every(result=>result.endpoints!.every(endpoint=>endpoint.bandwidth!.bytes>0))).toBe(true);
  await vi.advanceTimersByTimeAsync(6000);
  expect(publish).not.toHaveBeenCalled();
  const resume=(await bridge.bootstrap()).resume;
  expect(resume.options.find(option=>option.stage==="bandwidth")?.available).toBe(true);
  expect(resume.options.find(option=>option.stage==="ranking")?.available).toBe(false);
  await bridge.publishRun(summary!.runId,"all");
  await vi.advanceTimersByTimeAsync(200);
  expect(publish).toHaveBeenCalled();
  const kept=summary!;
  for(const explicit of [false,true]){
    await bridge.startRun(request);
    await vi.advanceTimersByTimeAsync(3120);
    if(explicit)await bridge.cancelRun(false);else await bridge.cancelRun();
    expect(summary!.state).toBe("cancelled");
    expect(summary!.partialResults).toBe(false);
    expect(summary!.results).toEqual([]);
  }
  const history=(await bridge.bootstrap()).history;
  expect(history.find(value=>value.runId===kept.runId)?.results).toHaveLength(3);
  expect(await bridge.cancelRun(true)).toBe(false);
  offProgress();offComplete();offPublish();
});
