import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Bootstrap, UpdateState } from "../src/types";
import { clearNative, mockNative, type NativeApp } from "./native";

let events:Record<string,(value:unknown)=>void>;
let api:NativeApp;
beforeEach(()=>{vi.resetModules();clearNative();events={};HTMLElement.prototype.scrollIntoView=vi.fn();});
afterEach(()=>{cleanup();clearNative();vi.restoreAllMocks();});

async function desktop(status:UpdateState["status"]="available",pending?:Promise<UpdateState>){
  const {defaultSettings,defaultPublishSettings}=await import("../src/lib/bridge");
  const data:Bootstrap={version:"0.9.0",settings:defaultSettings,publishSettings:defaultPublishSettings,sources:[],history:[],network:{interface:"test",ipv4:"",status:"online"},resume:{options:[]}};
  api={Bootstrap:vi.fn(async()=>data),UpdateStatus:vi.fn(async()=>pending??({currentVersion:"0.9.0",status,version:["available","ready"].includes(status)?"0.10.0":undefined})),CheckForUpdate:vi.fn(async()=>({currentVersion:"0.9.0",status:"available",version:"0.10.0",notes:"改善测速体验"})),DownloadUpdate:vi.fn(async()=>({currentVersion:"0.9.0",status:"downloading",version:"0.10.0",size:10485760})),RestartForUpdate:vi.fn(async()=>{})} as unknown as NativeApp;
  mockNative(api,events);
  const {actions}=await import("../src/store");
  await actions.init();
  const {UpdateSection}=await import("../src/features/settings/UpdateSection");
  render(<UpdateSection/>);
  return data;
}

it("reads startup state once, downloads on request, and waits for active work before restarting",async()=>{
  await desktop();
  await screen.findByRole("button",{name:"下载更新"});
  expect(api.UpdateStatus).toHaveBeenCalledOnce();
  expect(api.CheckForUpdate).not.toHaveBeenCalled();
  expect(api.DownloadUpdate).not.toHaveBeenCalled();
  const {actions}=await import("../src/store");
  await act(async()=>actions.init());
  expect(api.UpdateStatus).toHaveBeenCalledOnce();
  expect(api.CheckForUpdate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button",{name:"下载更新"}));
  await waitFor(()=>expect(api.DownloadUpdate).toHaveBeenCalledOnce());
  await act(async()=>events["update:state"]({currentVersion:"0.9.0",status:"downloading",version:"0.10.0",downloaded:5242880,size:10485760}));
  expect(screen.getByRole("progressbar",{name:"更新下载进度"})).toHaveAttribute("value","5242880");
  expect(screen.getByText("正在下载 5.0 MB / 10.0 MB")).toBeInTheDocument();
  await act(async()=>{
    events["run:progress"]({runId:"active",state:"running",startedAt:new Date().toISOString(),stages:[],failures:{}});
    events["update:state"]({currentVersion:"0.9.0",status:"ready",version:"0.10.0"});
  });
  expect(screen.getByRole("button",{name:"重启并更新"})).toBeDisabled();
  expect(screen.getByText("测速或发布任务结束后可重启更新")).toBeInTheDocument();
  await expect(actions.restartForUpdate()).rejects.toThrow("请等待");
  expect(api.RestartForUpdate).not.toHaveBeenCalled();
  await act(async()=>events["run:complete"]({runId:"active",state:"completed",startedAt:"",finishedAt:"",results:[],failures:{},publications:[],scoreWeights:{},reusedStages:[]}));
  await act(async()=>{
    events["publish:progress"]({runId:"active",result:{target:"github",state:"queued",items:0}});
    events["publish:progress"]({runId:"active",result:{target:"telegram",state:"running",items:0}});
    events["publish:progress"]({runId:"active",result:{target:"github",state:"succeeded",items:1}});
  });
  expect(screen.getByRole("button",{name:"重启并更新"})).toBeDisabled();
  await act(async()=>events["publish:progress"]({runId:"active",result:{target:"telegram",state:"failed",items:0}}));
  fireEvent.click(screen.getByRole("button",{name:"重启并更新"}));
  await waitFor(()=>expect(api.RestartForUpdate).toHaveBeenCalledOnce());
});

it("shows update errors and allows checking again",async()=>{
  await desktop("unavailable");
  await act(async()=>events["update:state"]({currentVersion:"0.9.0",status:"error",error:"下载连接中断"}));
  expect(screen.getByRole("alert")).toHaveTextContent("下载连接中断");
  fireEvent.click(screen.getByRole("button",{name:"重试检查"}));
  await screen.findByRole("button",{name:"下载更新"});
  expect(api.CheckForUpdate).toHaveBeenCalledOnce();
});

it("keeps browser previews explicit about unavailable updates",async()=>{
  const {App}=await import("../src/App");
  render(<App/>);
  await screen.findByRole("heading",{name:"测速工作台"});
  fireEvent.click(screen.getByRole("button",{name:"查看应用更新"}));
  await screen.findByText("浏览器预览不支持自动更新，请使用桌面应用");
  expect(screen.getByRole("button",{name:"检查更新"})).toBeDisabled();
  expect(screen.queryByRole("button",{name:"下载更新"})).not.toBeInTheDocument();
});

it("reports desktop initialization failures instead of showing mock data",async()=>{
  mockNative({Bootstrap:vi.fn(async()=>{throw new Error("RPC unavailable");})} as unknown as NativeApp,events);
  const {App}=await import("../src/App");
  render(<App/>);
  expect(await screen.findByRole("alert")).toHaveTextContent("桌面应用初始化失败");
  expect(screen.getByRole("alert")).toHaveTextContent("RPC unavailable");
  expect(screen.queryByRole("heading",{name:"测速工作台"})).not.toBeInTheDocument();
});

it("keeps a staged update ready when starting the restart helper fails",async()=>{
  await desktop("ready");
  vi.mocked(api.RestartForUpdate).mockRejectedValueOnce(new Error("无法启动更新 helper"));
  fireEvent.click(screen.getByRole("button",{name:"重启并更新"}));
  expect(await screen.findByRole("alert")).toHaveTextContent("无法启动更新 helper");
  expect(screen.getByRole("button",{name:"重启并更新"})).toBeEnabled();
  expect(api.DownloadUpdate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button",{name:"重启并更新"}));
  await waitFor(()=>expect(api.RestartForUpdate).toHaveBeenCalledTimes(2));
});

it("keeps a newer event when the startup status request finishes later",async()=>{
  let resolve!:(state:UpdateState)=>void;
  const pending=new Promise<UpdateState>(done=>{resolve=done;});
  await desktop("idle",pending);
  await act(async()=>events["update:state"]({currentVersion:"0.9.0",status:"ready",version:"0.10.0"}));
  await act(async()=>resolve({currentVersion:"0.9.0",status:"idle"}));
  expect(screen.getByRole("button",{name:"重启并更新"})).toBeEnabled();
  expect(api.CheckForUpdate).not.toHaveBeenCalled();
});
