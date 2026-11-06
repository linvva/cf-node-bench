import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "../src/App";
import { RunWorkspace } from "../src/features/run/RunWorkspace";
import { bridge, defaultPublishSettings, defaultSettings } from "../src/lib/bridge";
import * as store from "../src/store";
import { actions, getAppState } from "../src/store";
import type { PublishSaveRequest, PublishSettingsView, RunSummary } from "../src/types";

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
  it("shows the migration notice without requiring a private Worker domain",async()=>{
    const data=await bridge.bootstrap();vi.spyOn(bridge,"bootstrap").mockResolvedValue({...data,migrationNotice:"原发布配置已合并"});
    render(<App/>);expect(await screen.findByText("原发布配置已合并")).toBeInTheDocument();
  });
});
