import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "../src/App";
import { RunWorkspace } from "../src/features/run/RunWorkspace";
import { bridge, defaultPublishSettings, defaultSettings } from "../src/lib/bridge";
import * as store from "../src/store";
import { actions, getAppState } from "../src/store";
import type { PublishSaveRequest, PublishSettingsView, RunSummary, StageProgress } from "../src/types";

const request=(settings=defaultPublishSettings):PublishSaveRequest=>({settings:structuredClone(settings),cloudflareToken:"",githubToken:"",gistToken:"",telegramBotToken:"",telegramRelayKey:""});
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done;});return {promise,resolve};}
beforeEach(async()=>{await actions.init();await actions.saveSettings(defaultSettings);});
afterEach(()=>{cleanup();vi.restoreAllMocks();});

describe("shared publication and H3 requirement",()=>{
  it.each(["save","clear"] as const)("applies a delayed %s to the shared publication after changing the H3 requirement",async(operation)=>{
    const updated={...getAppState().publishSettings,gist:{...getAppState().publishSettings.gist,filename:"shared.txt"}};
    const delayed=deferred<PublishSettingsView>();
    const spy=operation==="save"?vi.spyOn(bridge,"savePublishSettings").mockReturnValueOnce(delayed.promise):vi.spyOn(bridge,"clearPublishCredential").mockReturnValueOnce(delayed.promise);
    const pending=operation==="save"?actions.savePublishSettings(request(updated)):actions.clearPublishCredential("github");
    if(operation==="save")expect(spy).toHaveBeenCalledWith(request(updated));else expect(spy).toHaveBeenCalledWith("github");
    await actions.setRequireH3(true);delayed.resolve(updated);await pending;
    expect(getAppState().settings.requireH3).toBe(true);expect(getAppState().publishSettings).toEqual(updated);
  });
  it("preserves other credential drafts when clearing one credential",async()=>{
    await actions.savePublishSettings({...request(),githubToken:"test-value"});
    render(<App/>);await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"发布"}));
    fireEvent.change(screen.getByLabelText("API Token"),{target:{value:"unsaved-draft"}});
    fireEvent.click(screen.getByRole("button",{name:"清除 Personal Access Token"}));
    await waitFor(()=>expect(getAppState().publishSettings.github.tokenConfigured).toBe(false));
    expect(screen.getByLabelText("API Token")).toHaveValue("unsaved-draft");
  });
  it("remembers the requirement and disables it during a run",async()=>{
    render(<App/>);await screen.findByRole("heading",{name:"测速工作台"});
    const toggle=screen.getByRole("tab",{name:"H3"});expect(toggle).toHaveAttribute("aria-selected","false");
    fireEvent.click(toggle);await waitFor(()=>expect(toggle).toHaveAttribute("aria-selected","true"));
    fireEvent.click(screen.getByRole("button",{name:"设置"}));fireEvent.click(screen.getByRole("button",{name:/测速工作台/}));
    expect(screen.getByRole("tab",{name:"H3"})).toHaveAttribute("aria-selected","true");
    fireEvent.click(screen.getByRole("button",{name:/开始测速/}));
    expect(screen.getByRole("tab",{name:"H3"})).toHaveAttribute("aria-disabled","true");
    await expect(actions.setRequireH3(false)).rejects.toThrow("测速中");
    fireEvent.click(await screen.findByRole("button",{name:/取消测速/}));
    await waitFor(()=>expect(screen.getByRole("tab",{name:"H3"})).not.toHaveAttribute("aria-disabled","true"));
  });
  it("waits for the H3 requirement to be saved before starting a run",async()=>{
    render(<App/>);await screen.findByRole("heading",{name:"测速工作台"});
    const delayed=deferred<void>();
    const save=bridge.saveSettings.bind(bridge);
    vi.spyOn(bridge,"saveSettings").mockImplementationOnce(async value=>{await delayed.promise;await save(value);});
    fireEvent.click(screen.getByRole("tab",{name:"H3"}));
    expect(screen.getByRole("button",{name:/开始测速/})).toBeDisabled();
    expect(screen.getByRole("button",{name:"选择测速起点"})).toBeDisabled();
    delayed.resolve();
    await waitFor(()=>expect(screen.getByRole("button",{name:/开始测速/})).toBeEnabled());
    expect(screen.getByRole("tab",{name:"H3"})).toHaveAttribute("aria-selected","true");
  });
  it("opens the migrated current result while preserving chronological history",async()=>{
    const data=await bridge.bootstrap();
    const older={runId:"old-active",mode:"h3",state:"completed",results:[],failures:{},publications:[],scoreWeights:defaultSettings.scoreWeights,reusedStages:[],startedAt:"2026-11-04T00:00:00Z",finishedAt:"2026-11-04T00:01:00Z"} as RunSummary;
    const newer={...older,runId:"new-other",mode:"tcp_https" as const,startedAt:"2026-11-05T00:00:00Z",finishedAt:"2026-11-05T00:01:00Z"};
    const bootstrap=vi.spyOn(bridge,"bootstrap").mockResolvedValue({...data,currentResultRunId:older.runId,history:[newer,older]});
    await actions.init();
    expect(getAppState().current?.runId).toBe(older.runId);
    expect(getAppState().history.map(summary=>summary.runId)).toEqual([newer.runId,older.runId]);
    bootstrap.mockResolvedValue({...data,currentResultRunId:"removed-run",history:[newer,older]});
    await actions.init();
    expect(getAppState().current?.runId).toBe(newer.runId);
  });
  it("shows zero current counters instead of borrowing the previous run values",()=>{
    const previous={mode:"h3",logicalCandidates:99,physicalCandidates:199,downloadedBytes:999*1048576,runId:"previous",state:"completed",results:[],failures:{},publications:[],scoreWeights:defaultSettings.scoreWeights,reusedStages:[],startedAt:new Date().toISOString(),finishedAt:new Date().toISOString()} as RunSummary;
    const current={...getAppState(),settings:{...defaultSettings,requireH3:true},current:previous,running:true,progress:{runId:"new",mode:"h3" as const,state:"running",startedAt:new Date().toISOString(),stages:[],failures:{}}};
    vi.spyOn(store,"useAppStore").mockImplementation(selector=>selector(current));
    render(<RunWorkspace/>);
    expect(screen.getByLabelText("H3 检测数量")).toHaveTextContent("逻辑候选：0实际 IPv4：0累计下载：0.0 MiB");
  });
  it.each(["running","completed"] as const)("counts skipped attempts as resolved work without reporting them as executed when %s",state=>{
    const completed=state==="running"?2:3,skipped=completed*2;
    const current={...getAppState(),settings:{...defaultSettings,requireH3:true},running:state==="running",progress:{runId:"early-stop",mode:"h3" as const,state,startedAt:new Date().toISOString(),stages:[{name:"quic",input:3,passed:0,failed:completed,attemptsCompleted:completed,attemptsSkipped:skipped,attemptsTotal:9,durationMs:2000,state}],failures:{quic_timeout:completed}}};
    vi.spyOn(store,"useAppStore").mockImplementation(selector=>selector(current));
    render(<RunWorkspace/>);
    expect(Number(screen.getByRole("progressbar",{name:"QUIC进度"}).getAttribute("aria-valuenow"))).toBeCloseTo((completed+skipped)/9*100);
    expect(screen.getByText(`提前跳过 ${skipped} 次`)).toBeInTheDocument();
    expect(screen.getByLabelText("当前测速进度")).toHaveTextContent(`${completed} ${state==="running"?"/ 3 节点":"失败"}`);
    if(state==="running"){
      expect(screen.getByText("2 / 9 次探测 · 提前跳过 4 次")).toBeInTheDocument();
      expect(screen.getByText("1 次/秒")).toBeInTheDocument();
      expect(screen.getByText("1 秒")).toBeInTheDocument();
    }
  });
  it.each([undefined,"tcp_https"] as const)("shows new TCP progress over previous H3 results when mode is %s",mode=>{
    const previous={mode:"h3",logicalCandidates:99,physicalCandidates:199,downloadedBytes:999*1048576,runId:"previous",state:"completed",results:[],failures:{},publications:[],scoreWeights:defaultSettings.scoreWeights,reusedStages:[],startedAt:new Date().toISOString(),finishedAt:new Date().toISOString()} as RunSummary;
    const current={...getAppState(),settings:{...defaultSettings,requireH3:false},current:previous,running:true,progress:{runId:"new",mode,state:"running",startedAt:new Date().toISOString(),stages:[],failures:{}}};
    vi.spyOn(store,"useAppStore").mockImplementation(selector=>selector(current));
    render(<RunWorkspace/>);
    expect(screen.queryByLabelText("H3 检测数量")).not.toBeInTheDocument();
    expect(screen.getByText("TCP 通过")).toBeInTheDocument();
    expect(screen.getByText("HTTPS 通过")).toBeInTheDocument();
    expect(screen.queryByText("QUIC 通过")).not.toBeInTheDocument();
    expect(screen.getByRole("heading",{name:"测速结果 · H3"})).toBeInTheDocument();
  });
  it.each(["tcp_https","h3"] as const)("shows the selected HTTP pool and download target independently from queued work in %s",mode=>{
    const http:StageProgress={name:mode==="h3"?"http3":"https",input:500,passed:350,failed:150,selected:200,durationMs:5000,state:"completed"};
    const download:StageProgress={name:"bandwidth",input:3,passed:0,failed:0,attemptsCompleted:0,attemptsTotal:3,durationMs:1000,state:"running",queue:{pool:200,target:30,planned:30,active:3,pending:27,passed:0}};
    const current={...getAppState(),running:true,progress:{runId:"queue",mode,state:"running",startedAt:new Date().toISOString(),stages:[http,download],failures:{}}};
    vi.spyOn(store,"useAppStore").mockImplementation(selector=>selector(current));
    const view=render(<RunWorkspace/>);
    expect(screen.getByLabelText("候选路径")).toHaveTextContent(`200${mode==="h3"?"新节点入选":"HTTPS 入选"}`);
    expect(screen.getByLabelText("带宽新节点队列")).toHaveTextContent("新节点候选池：200累计安排：30待测：27在测：3新节点通过：0 / 30");
    expect(screen.getByRole("progressbar",{name:"带宽进度"})).toHaveAttribute("aria-valuenow","0");
    Object.assign(download,{input:33,passed:18,failed:12,attemptsCompleted:30,attemptsTotal:33,queue:{pool:200,target:30,planned:50,active:3,pending:17,passed:18}});
    view.rerender(<RunWorkspace/>);
    expect(screen.getByLabelText("带宽新节点队列")).toHaveTextContent("累计安排：50待测：17在测：3新节点通过：18 / 30");
    expect(screen.getByRole("progressbar",{name:"带宽进度"})).toHaveAttribute("aria-valuenow","60");
    expect(screen.getByText("动态补测")).toBeInTheDocument();
    Object.assign(download,{input:44,passed:30,failed:12,queue:{pool:200,target:30,planned:50,active:2,pending:0,passed:30}});
    view.rerender(<RunWorkspace/>);
    expect(screen.getByText("等待在测完成")).toBeInTheDocument();
    expect(screen.getByRole("progressbar",{name:"带宽进度"})).toHaveAttribute("aria-valuenow","100");
    http.selected=0;
    view.rerender(<RunWorkspace/>);
    expect(screen.getByLabelText("候选路径")).toHaveTextContent(`0${mode==="h3"?"新节点入选":"HTTPS 入选"}`);
    http.selected=2;
    current.running=false;
    current.progress.state="completed";
    Object.assign(download,{input:2,passed:2,failed:0,state:"completed",queue:{pool:2,target:30,planned:2,active:0,pending:0,passed:2}});
    view.rerender(<RunWorkspace/>);
    expect(screen.getByLabelText("带宽新节点队列")).toHaveTextContent("新节点通过：2 / 30");
    expect(screen.getByText("候选已耗尽，未达到目标")).toBeInTheDocument();
    expect(Number(screen.getByRole("progressbar",{name:"带宽进度"}).getAttribute("aria-valuenow"))).toBeCloseTo(2/30*100);
    expect(screen.queryByText("已达目标")).not.toBeInTheDocument();
  });
  it("shows the migration notice without requiring a private Worker domain",async()=>{
    const data=await bridge.bootstrap();vi.spyOn(bridge,"bootstrap").mockResolvedValue({...data,migrationNotice:"原发布配置已合并"});
    render(<App/>);expect(await screen.findByText("原发布配置已合并")).toBeInTheDocument();
  });
});
