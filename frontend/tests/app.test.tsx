import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "@heroui/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "../src/App";
import { ResultsTable } from "../src/features/results/ResultsTable";
import { countryOptions } from "../src/features/settings/countries";
import { validateBandwidthTestURL, validateSettings } from "../src/features/settings/SettingsPage";
import { bandwidthTestURLPresets, DEFAULT_BANDWIDTH_TEST_URL } from "../src/lib/bandwidthTestUrls";
import { actions, getAppState } from "../src/store";
import type { RunSummary } from "../src/types";

afterEach(()=>{cleanup();document.documentElement.className="";localStorage.clear();});

describe("CF Node Bench workspace",()=>{
  it("starts and cancels a run without shifting the stage layout",async()=>{
    render(<App/>);
    await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:/开始测速/}));
    expect(screen.getByLabelText("测速阶段").children[0]).toHaveAttribute("data-state","running");
    expect(screen.getByLabelText("当前测速进度")).toHaveTextContent("获取数据源进行中");
    expect(screen.getByRole("progressbar",{name:"数据源进度"})).toBeInTheDocument();
    expect(screen.getByLabelText("候选漏斗")).toBeInTheDocument();
    expect(await screen.findByRole("button",{name:/取消测速/})).toBeInTheDocument();
    expect(screen.getByLabelText("测速阶段").children).toHaveLength(6);
    fireEvent.click(screen.getByRole("button",{name:/取消测速/}));
    await waitFor(()=>expect(screen.getByRole("button",{name:/开始测速/})).toBeInTheDocument());
    expect(screen.getByRole("heading",{name:"测速已取消"})).toBeInTheDocument();
  });

  it("shows a clear settings relationship error",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"设置"}));
    const tcp=await screen.findByLabelText("TCP 候选池");
    fireEvent.change(tcp,{target:{value:"10"}});
    fireEvent.change(screen.getByLabelText("带宽目标通过数"),{target:{value:"30"}});
    fireEvent.click(screen.getByRole("button",{name:/保存设置/}));
    expect(await screen.findByText("带宽目标通过数不能大于 TCP 候选数")).toBeInTheDocument();
  });

  it("shows advanced probe controls and rejects conflicting country filters",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"设置"}));
    expect(await screen.findByLabelText("最大下载量")).toHaveValue(20);
    expect(screen.getByLabelText("数据源重试次数")).toHaveValue(2);
    expect(screen.getByLabelText("允许国家选择器")).toBeInTheDocument();
    expect(screen.getByLabelText("排除国家选择器")).toBeInTheDocument();
    expect(countryOptions.find((country)=>country.code==="CN")?.name).toContain("中国");
    const settings={...getAppState().settings,allowedCountries:["US"],blockedCountries:["US"]};
    expect(validateSettings(settings)).toBe("国家 US 不能同时出现在允许和排除列表");
  });

  it("selects a bandwidth preset and saves a custom HTTPS URL",async()=>{
    const save=vi.spyOn(actions,"saveSettings").mockResolvedValueOnce();
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"设置"}));
    const input=await screen.findByLabelText("下载测速 URL");
    expect(input).toHaveValue(DEFAULT_BANDWIDTH_TEST_URL);
    fireEvent.click(screen.getByRole("button",{name:/常用地址/}));
    fireEvent.click(await screen.findByRole("option",{name:/Parallels.*318 MiB/}));
    expect(input).toHaveValue(bandwidthTestURLPresets[1].url);
    fireEvent.change(input,{target:{value:"  https://downloads.example.test/custom.bin?token=value  "}});
    fireEvent.click(screen.getByRole("button",{name:/保存设置/}));
    await waitFor(()=>expect(save).toHaveBeenCalledWith(expect.objectContaining({bandwidthTestUrl:"https://downloads.example.test/custom.bin?token=value"})));
    save.mockRestore();
  });

  it("validates bandwidth test URLs before saving",()=>{
    expect(validateBandwidthTestURL("")).toBe("下载测速地址不能为空");
    expect(validateBandwidthTestURL("http://downloads.example.test/file.bin")).toBe("下载测速地址必须是完整的 HTTPS URL");
    expect(validateBandwidthTestURL("https://user:secret@downloads.example.test/file.bin")).toBe("下载测速地址不能包含用户名或密码");
    expect(validateBandwidthTestURL("https://downloads.example.test/file.bin#part")).toBe("下载测速地址不能包含片段");
    expect(validateBandwidthTestURL("https://downloads.example.test/file.bin?token=value")).toBe("");
  });

  it("adds an editable HTTP source",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"数据源"}));
    fireEvent.click(await screen.findByRole("button",{name:/添加数据源/}));
    fireEvent.change(screen.getByLabelText("数据源名称"),{target:{value:"本地源"}});
    fireEvent.change(screen.getByLabelText("数据源 URL"),{target:{value:"http://127.0.0.1:8080/nodes"}});
    fireEvent.click(screen.getByRole("button",{name:"保存"}));
    expect(await screen.findByText("本地源")).toBeInTheDocument();
  });

  it("switches Cloudflare A and TXT controls with a shared format preview",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"发布"}));
    expect(await screen.findByRole("heading",{name:"结果发布"})).toBeInTheDocument();
    expect(screen.getByText("自动发布尚未生效")).toBeInTheDocument();
    expect(screen.getByText(/启用目标并保存设置后，测速成功完成时会自动发布/)).toBeInTheDocument();
    expect(screen.getByText("启用 Cloudflare 代理")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab",{name:"TXT 记录"}));
    expect(screen.queryByText("启用 Cloudflare 代理")).not.toBeInTheDocument();
    expect(screen.getByText("104.18.1.20:443#US|HTTP44ms|186Mbps")).toBeInTheDocument();
  });

  it("validates an enabled publish target before saving",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"发布"}));
    fireEvent.click(await screen.findByRole("switch",{name:"启用 Cloudflare DNS"}));
    fireEvent.click(screen.getByRole("button",{name:/保存设置/}));
    expect(await screen.findByText("启用 Cloudflare 前必须填写 Token、Zone ID 和记录名")).toBeInTheDocument();
  });

  it("guides Cloudflare and GitHub repository setup with minimal permissions",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"发布"}));
    const open=vi.spyOn(window,"open").mockImplementation(()=>null);
    fireEvent.click(await screen.findByRole("button",{name:"Cloudflare 配置引导"}));
    expect(await screen.findByRole("heading",{name:"配置 Cloudflare DNS"})).toBeInTheDocument();
    expect(screen.getByText(/将资源范围限制为目标 Zone/)).toHaveTextContent("Edit zone DNS");
    expect(screen.getByText(/Managed by CF Node Bench/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"打开控制台"}));
    expect(open).toHaveBeenCalledWith("https://dash.cloudflare.com/","_blank","noopener,noreferrer");
    fireEvent.click(screen.getByRole("button",{name:"创建 API Token"}));
    expect(open).toHaveBeenCalledWith("https://dash.cloudflare.com/profile/api-tokens","_blank","noopener,noreferrer");
    fireEvent.click(screen.getByRole("button",{name:"关闭 Cloudflare 配置引导"}));
    await waitFor(()=>expect(screen.queryByRole("heading",{name:"配置 Cloudflare DNS"})).not.toBeInTheDocument());
    fireEvent.click(screen.getByRole("button",{name:"GitHub 仓库配置引导"}));
    expect(await screen.findByRole("heading",{name:"配置 GitHub 仓库"})).toBeInTheDocument();
    expect(screen.getByText(/文件无需预建/)).toBeInTheDocument();
    expect(screen.getByText(/Repository permissions/)).toHaveTextContent("Read and write");
    fireEvent.click(screen.getByRole("button",{name:"创建仓库"}));
    expect(open).toHaveBeenCalledWith("https://github.com/new","_blank","noopener,noreferrer");
    fireEvent.click(screen.getByRole("button",{name:"创建 Token"}));
    expect(open).toHaveBeenCalledWith("https://github.com/settings/personal-access-tokens/new","_blank","noopener,noreferrer");
    fireEvent.click(screen.getByRole("button",{name:"关闭 GitHub 仓库配置引导"}));
    await waitFor(()=>expect(screen.queryByRole("heading",{name:"配置 GitHub 仓库"})).not.toBeInTheDocument());
    open.mockRestore();
  });

  it("keeps Gist separate from repository publishing and validates its credentials",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"发布"}));
    expect(await screen.findByRole("switch",{name:"启用 GitHub 仓库"})).toBeInTheDocument();
    expect(screen.getByText(/Secret Gist 只是未公开列出/)).toBeInTheDocument();
    const open=vi.spyOn(window,"open").mockImplementation(()=>null);
    fireEvent.click(screen.getByRole("button",{name:"Gist 配置引导"}));
    expect(await screen.findByRole("heading",{name:"配置 GitHub Gist"})).toBeInTheDocument();
    expect(screen.getByText(/新建文件/)).toHaveTextContent("ip.txt");
    expect(screen.getByText(/Account permissions/)).toHaveTextContent("Gists");
    fireEvent.click(screen.getByRole("button",{name:"创建 Gist"}));
    expect(open).toHaveBeenCalledWith("https://gist.github.com/","_blank","noopener,noreferrer");
    fireEvent.click(screen.getByRole("button",{name:"创建 Token"}));
    expect(open).toHaveBeenCalledWith("https://github.com/settings/personal-access-tokens/new","_blank","noopener,noreferrer");
    fireEvent.click(screen.getByRole("button",{name:"关闭 Gist 配置引导"}));
    await waitFor(()=>expect(screen.queryByRole("heading",{name:"配置 GitHub Gist"})).not.toBeInTheDocument());
    open.mockRestore();
    fireEvent.click(screen.getByRole("switch",{name:"启用 GitHub Gist"}));
    fireEvent.click(screen.getByRole("button",{name:/保存设置/}));
    expect(await screen.findByText("启用 Gist 前必须填写 Token、Gist ID 和文件名")).toBeInTheDocument();
  });

  it("shows dedicated Telegram relay fields and validates its separate key",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"发布"}));
    fireEvent.click(await screen.findByRole("tab",{name:"专属中继"}));
    expect(screen.getByLabelText("中继 URL")).toBeInTheDocument();
    expect(screen.getByLabelText("中继访问密钥")).toBeInTheDocument();
    expect(screen.getByText(/只使用部署在自己账户下的专属中继/)).toBeInTheDocument();
    const clipboard=vi.spyOn(navigator.clipboard,"writeText");
    fireEvent.click(screen.getByRole("button",{name:"部署 Worker"}));
    expect(await screen.findByRole("heading",{name:"部署 Telegram 专属中继"})).toBeInTheDocument();
    expect(screen.getByLabelText("worker.js 代码")).toHaveTextContent("ALLOWED_METHODS");
    fireEvent.click(screen.getByRole("button",{name:"复制代码"}));
    await waitFor(()=>expect(clipboard).toHaveBeenCalledWith(expect.stringContaining("api.telegram.org")));
    fireEvent.click(screen.getByRole("button",{name:"关闭部署引导"}));
    await waitFor(()=>expect(screen.queryByRole("heading",{name:"部署 Telegram 专属中继"})).not.toBeInTheDocument());
    clipboard.mockRestore();
    fireEvent.click(screen.getByRole("switch",{name:"启用 Telegram Bot"}));
    fireEvent.change(screen.getByLabelText("Chat ID"),{target:{value:"123"}});
    fireEvent.change(screen.getByLabelText("Bot Token"),{target:{value:"123456:token"}});
    fireEvent.click(screen.getByRole("button",{name:/保存设置/}));
    expect(await screen.findByText("启用 Telegram 专属中继前必须填写中继 URL 和访问密钥")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab",{name:"直连 Telegram"}));
    expect(screen.queryByLabelText("中继 URL")).not.toBeInTheDocument();
  });

  it("retries a failed publication from the result view",async()=>{
    const retry=vi.spyOn(actions,"publishRun").mockResolvedValueOnce();
    const summary={runId:"run-publish",startedAt:new Date().toISOString(),finishedAt:new Date().toISOString(),state:"completed",results:[],failures:{},publications:[{target:"github",state:"failed",items:0,message:"denied"}]} as RunSummary;
    render(<ResultsTable summary={summary}/>);
    fireEvent.click(screen.getByRole("button",{name:"重试 github"}));
    await waitFor(()=>expect(retry).toHaveBeenCalledWith("run-publish","github"));
    retry.mockRestore();
  });

  it("copies a stable latest-content URL for Gist results",async()=>{
    const rawURL="https://gist.githubusercontent.com/linvva/abc123/raw/1234567890abcdef1234567890abcdef12345678/ip.txt";
    const clipboard=vi.spyOn(navigator.clipboard,"writeText"); const success=vi.spyOn(toast,"success");
    const summary={runId:"run-gist",startedAt:new Date().toISOString(),finishedAt:new Date().toISOString(),state:"completed",results:[],failures:{},publications:[{target:"gist",state:"succeeded",items:2,url:rawURL,message:"Gist 文件已更新"}]} as RunSummary;
    render(<ResultsTable summary={summary}/>);
    expect(screen.getByRole("link",{name:"打开 Gist Raw 文件"})).toHaveAttribute("href",rawURL);
    fireEvent.click(screen.getByRole("button",{name:"复制固定 Gist Raw 地址"}));
    await waitFor(()=>expect(clipboard).toHaveBeenCalledWith("https://gist.githubusercontent.com/linvva/abc123/raw/ip.txt"));
    expect(success).toHaveBeenCalledWith("已复制固定 Raw 地址；它始终指向 Gist 最新内容，更新后可能有短暂缓存");
    expect(screen.getByText("Gist")).toBeInTheDocument();
    clipboard.mockRestore(); success.mockRestore();
  });
});
