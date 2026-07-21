import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { App } from "../src/App";
import { countryOptions } from "../src/features/settings/countries";
import { validateSettings } from "../src/features/settings/SettingsPage";
import { getAppState } from "../src/store";

afterEach(()=>{cleanup();document.documentElement.className="";localStorage.clear();});

describe("CF Node Bench workspace",()=>{
  it("starts and cancels a run without shifting the stage layout",async()=>{
    render(<App/>);
    await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:/开始测速/}));
    expect(screen.getByLabelText("测速阶段").children[0]).toHaveAttribute("data-state","running");
    expect(await screen.findByRole("button",{name:/取消测速/})).toBeInTheDocument();
    expect(screen.getByLabelText("测速阶段").children).toHaveLength(6);
    fireEvent.click(screen.getByRole("button",{name:/取消测速/}));
    await waitFor(()=>expect(screen.getByRole("button",{name:/开始测速/})).toBeInTheDocument());
    expect(screen.getByText("测速已取消")).toBeInTheDocument();
  });

  it("shows a clear settings relationship error",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"设置"}));
    const tcp=await screen.findByLabelText("TCP 候选池");
    fireEvent.change(tcp,{target:{value:"10"}});
    fireEvent.change(screen.getByLabelText("带宽候选池"),{target:{value:"30"}});
    fireEvent.click(screen.getByRole("button",{name:/保存设置/}));
    expect(await screen.findByText("带宽候选数不能大于 TCP 候选数")).toBeInTheDocument();
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

  it("adds an editable HTTP source",async()=>{
    render(<App/>); await screen.findByRole("heading",{name:"测速工作台"});
    fireEvent.click(screen.getByRole("button",{name:"数据源"}));
    fireEvent.click(await screen.findByRole("button",{name:/添加数据源/}));
    fireEvent.change(screen.getByLabelText("数据源名称"),{target:{value:"本地源"}});
    fireEvent.change(screen.getByLabelText("数据源 URL"),{target:{value:"http://127.0.0.1:8080/nodes"}});
    fireEvent.click(screen.getByRole("button",{name:"保存"}));
    expect(await screen.findByText("本地源")).toBeInTheDocument();
  });
});
