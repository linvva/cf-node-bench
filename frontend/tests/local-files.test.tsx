import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Bootstrap, RunSummary } from "../src/types";

let data:Bootstrap;
let events:Record<string,(value:unknown)=>void>;
let drop:((x:number,y:number,paths:string[])=>void)|undefined;
let api:NonNullable<NonNullable<NonNullable<Window["go"]>["main"]>["App"]>;
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done;});return {promise,resolve};}
beforeEach(async()=>{
  vi.resetModules();events={};drop=undefined;
  api={Bootstrap:vi.fn(async()=>data),SelectLocalFiles:vi.fn(async()=>[]),StartLocalFiles:vi.fn(async()=>"local-1")} as unknown as typeof api;
  window.go={main:{App:api}};
  window.runtime={EventsOn:(name,handler)=>{events[name]=handler;return ()=>{delete events[name];};},OnFileDrop:vi.fn(handler=>{drop=handler;}),OnFileDropOff:vi.fn(()=>{drop=undefined;})};
  const {defaultSettings,defaultPublishSettings}=await import("../src/lib/bridge");
  data={settings:structuredClone(defaultSettings),publishSettings:structuredClone(defaultPublishSettings),sources:[{id:"http",name:"原 HTTP 源",url:"https://example.test/nodes",enabled:true,nodeCount:12}],history:[],network:{interface:"test",ipv4:"",status:"online"},resume:{options:[]}};
});
afterEach(()=>{cleanup();delete window.go;delete window.runtime;vi.restoreAllMocks();});

it("cancels native selection without starting and blocks competing tasks during selection",async()=>{
  const selected=deferred<string[]|null>();vi.mocked(api.SelectLocalFiles).mockReturnValue(selected.promise);
  const {App}=await import("../src/App");const {actions,getAppState}=await import("../src/store");
  render(<App/>);await screen.findByRole("heading",{name:"测速工作台"});
  fireEvent.click(screen.getByRole("button",{name:"本地文件测速"}));
  expect(screen.getByRole("button",{name:"开始测速"})).toBeDisabled();
  expect(screen.getByRole("tab",{name:"H3"})).toHaveAttribute("aria-disabled","true");
  await expect(actions.startLocal(["another.csv"])).rejects.toThrow("准备中");
  await expect(actions.start()).rejects.toThrow("准备中");
  await expect(actions.setRequireH3(true)).rejects.toThrow("准备中");
  await act(async()=>selected.resolve(null));
  expect(api.StartLocalFiles).not.toHaveBeenCalled();expect(getAppState().sources).toEqual(data.sources);
  expect(getAppState().running).toBe(false);expect(screen.getByRole("button",{name:"本地文件测速"})).toBeEnabled();
});

it("uses only native dropped paths once, locks the running mode, and labels local history",async()=>{
  data.settings.requireH3=true;
  const {App}=await import("../src/App");const {getAppState}=await import("../src/store");
  render(<App/>);await screen.findByRole("heading",{name:"测速工作台"});
  fireEvent.click(screen.getByRole("button",{name:"数据源"}));
  fireEvent.dragEnter(window,{dataTransfer:{types:["Files"]}});expect(screen.getByText("释放开始测速")).toBeInTheDocument();
  fireEvent.drop(window,{dataTransfer:{types:["Files"],files:[new File(["ignored DOM content"],"unused.csv")]}});
  const paths=["C:\\节点\\nodes.csv","C:\\Downloads\\nodes.txt"];
  await act(async()=>drop?.(10,20,paths));
  expect(api.StartLocalFiles).toHaveBeenCalledExactlyOnceWith(paths);expect(api.SelectLocalFiles).not.toHaveBeenCalled();
  expect(await screen.findByRole("heading",{name:"测速工作台"})).toBeInTheDocument();
  expect(screen.getByRole("tab",{name:"H3"})).toHaveAttribute("aria-disabled","true");
  expect(getAppState().progress).toMatchObject({localFiles:true,mode:"h3"});
  expect(screen.getByLabelText("候选路径")).toHaveTextContent("仅测试本批文件中的候选节点");
  await act(async()=>drop?.(10,20,["other.json"]));expect(api.StartLocalFiles).toHaveBeenCalledTimes(1);
  const summary:RunSummary={runId:"local-1",localFiles:true,mode:"h3",state:"completed",results:[],failures:{},publications:[],scoreWeights:data.settings.scoreWeights,reusedStages:[],startedAt:new Date().toISOString(),finishedAt:new Date().toISOString()};
  data.history=[summary];data.currentResultRunId=summary.runId;
  await act(async()=>events["run:complete"](summary));
  expect(screen.getByRole("heading",{name:"测速结果 · H3 · 本地文件"})).toBeInTheDocument();
  expect(screen.getByRole("combobox",{name:"查看历史运行"})).toHaveTextContent("本地文件");
  expect(getAppState().sources).toEqual(data.sources);expect(getAppState().settings.requireH3).toBe(true);
});

it("shows a local parsing error without replacing history or saving a source",async()=>{
  vi.mocked(api.SelectLocalFiles).mockResolvedValue(["broken.csv"]);
  vi.mocked(api.StartLocalFiles).mockRejectedValue(new Error("broken.csv：没有有效节点"));
  const {App}=await import("../src/App");const {getAppState}=await import("../src/store");
  render(<App/>);await screen.findByRole("heading",{name:"测速工作台"});
  fireEvent.click(screen.getByRole("button",{name:"本地文件测速"}));
  await waitFor(()=>expect(getAppState().error).toContain("没有有效节点"));
  expect(api.StartLocalFiles).toHaveBeenCalledExactlyOnceWith(["broken.csv"]);
  expect(getAppState().history).toHaveLength(0);expect(getAppState().sources).toEqual(data.sources);
  expect(screen.getByRole("button",{name:"本地文件测速"})).toBeEnabled();
});
