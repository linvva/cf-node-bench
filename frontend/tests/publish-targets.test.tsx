import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "../src/App";
import { latestGistRawURL, validatePublishSettings } from "../src/features/publish/PublishPage";
import { bridge, defaultPublishSettings, defaultSettings, normalizeBootstrap, publishSettingsForMode, publishTargetConfigured } from "../src/lib/bridge";
import { actions, getAppState } from "../src/store";
import type { Bootstrap, PublicationUpdate, PublishSaveRequest, PublishSettingsView, RunSummary } from "../src/types";
const tokens={cloudflareToken:"",githubToken:"",gistToken:"",telegramBotToken:"",telegramRelayKey:""};
const request=(settings:PublishSettingsView):PublishSaveRequest=>({settings,...tokens});
const targets=():PublishSettingsView=>({...structuredClone(defaultPublishSettings),cloudflare:{...defaultPublishSettings.cloudflare,recordName:"cf.example.com",h3:{recordName:"h3.example.com",recordType:"TXT"}},github:{...defaultPublishSettings.github,path:"ip.txt",h3:{path:"h3.txt"}},gist:{...defaultPublishSettings.gist,gistId:"abc123",filename:"ip.txt",h3:{gistId:"abc123",filename:"h3.txt"}}});
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.useRealTimers();});
describe("separate publication targets with shared credentials",()=>{
  it("leaves missing targets empty without inheriting ordinary destinations",()=>{
    const old=JSON.parse(JSON.stringify(targets()));delete old.cloudflare.h3;delete old.github.h3;delete old.gist.h3;
    const normalized=normalizeBootstrap({publishSettings:old} as Bootstrap).publishSettings;
    expect(normalized.cloudflare.h3).toEqual({recordName:"",recordType:"TXT"});expect(normalized.github.h3).toEqual({path:""});expect(normalized.gist.h3).toEqual({gistId:"",filename:""});
    expect(defaultPublishSettings.github.path).toBe("");expect(defaultPublishSettings.gist.filename).toBe("");
    for(const target of ["cloudflare","github","gist"] as const){expect(publishTargetConfigured(normalized,target,"h3")).toBe(false);expect(publishTargetConfigured(normalized,target,"tcp_https")).toBe(true);}
  });
  it("accepts empty destinations and H3-only configuration but rejects incomplete Gist pairs",()=>{
    expect(validatePublishSettings(defaultPublishSettings,tokens)).toBe("");
    const h3Only=structuredClone(defaultPublishSettings);h3Only.gist={...h3Only.gist,enabled:true,tokenConfigured:true,h3:{gistId:"def456",filename:"h3.txt"}};
    expect(validatePublishSettings(h3Only,tokens)).toBe("");h3Only.gist.h3.filename="";expect(validatePublishSettings(h3Only,tokens)).toContain("同时填写或同时留空");
    h3Only.gist.h3.gistId="";expect(validatePublishSettings(h3Only,tokens)).toBe("");h3Only.gist.gistId="abc123";expect(validatePublishSettings(h3Only,tokens)).toContain("普通 Gist");
  });
  it("rejects collisions and malformed targets while allowing either Gist separation strategy",()=>{
    const valid=targets();expect(validatePublishSettings(valid,tokens)).toBe("");
    const cf=structuredClone(valid);cf.cloudflare.h3.recordName=" CF.EXAMPLE.COM. ";expect(validatePublishSettings(cf,tokens)).toContain("记录名必须与普通目标不同");
    for(const path of ["ip.txt","/h3.txt","../h3.txt","a/../h3.txt","a//h3.txt","a/./h3.txt"]){const current=structuredClone(valid);current.github.h3.path=path;expect(validatePublishSettings(current,tokens)).toContain("H3 GitHub");}
    const gist=structuredClone(valid);gist.gist.h3.gistId="ABC123";gist.gist.h3.filename="ip.txt";expect(validatePublishSettings(gist,tokens)).toContain("不同的 Gist ID 或文件名");gist.gist.h3.gistId="def456";expect(validatePublishSettings(gist,tokens)).toBe("");
    for(const gistId of ["https://gist.github.com/id","bad\\id","bad id","bad?id"]){const current=structuredClone(valid);current.gist.h3.gistId=gistId;expect(validatePublishSettings(current,tokens)).toContain("H3 Gist ID");}
    for(const filename of ["..","a/h3.txt","a\\h3.txt","bad\nname.txt"]){const current=structuredClone(valid);current.gist.h3.filename=filename;expect(validatePublishSettings(current,tokens)).toContain("H3 Gist 文件名");}
  });
  it("resolves a copied destination without altering shared accounts or falling back",()=>{
    const settings=targets();settings.cloudflare.tokenConfigured=true;settings.cloudflare.zoneId="shared-zone";settings.github.owner="owner";settings.github.repository="repo";settings.gist.tokenConfigured=true;
    const h3=publishSettingsForMode(settings,"h3");expect(h3.cloudflare).toMatchObject({recordName:"h3.example.com",recordType:"TXT",zoneId:"shared-zone",tokenConfigured:true});expect(h3.github).toMatchObject({owner:"owner",repository:"repo",branch:"main",path:"h3.txt"});expect(h3.gist).toMatchObject({gistId:"abc123",filename:"h3.txt",tokenConfigured:true});
    h3.gist.h3.filename="changed.txt";expect(settings.gist.h3.filename).toBe("h3.txt");expect(publishSettingsForMode(settings,"tcp_https").gist.filename).toBe("ip.txt");settings.gist.h3={gistId:"",filename:""};expect(publishSettingsForMode(settings,"h3").gist.filename).toBe("");
  });
  it("routes auto publication, historical retry and queued snapshots by result protocol",async()=>{
    vi.useFakeTimers();const settings=targets();settings.gist.enabled=true;await bridge.savePublishSettings({...request(settings),gistToken:"shared-token"});
    const summaries:RunSummary[]=[];const updates:PublicationUpdate[]=[];const off=bridge.onComplete(summary=>summaries.push(summary));const offPublish=bridge.onPublish(update=>updates.push(update));
    for(const requireH3 of [false,true]){await bridge.saveSettings({...defaultSettings,requireH3});await bridge.startRun({startStage:"source",allowStaleCheckpoint:false});await vi.advanceTimersByTimeAsync(6000);}
    const [ordinary,h3]=summaries;const published=(id:string)=>updates.filter(update=>update.runId===id&&update.result.target==="gist").at(-1)!.result;
    expect(published(ordinary.runId).url).toBe("https://gist.githubusercontent.com/example/abc123/raw/ip.txt");expect(published(h3.runId).url).toBe("https://gist.githubusercontent.com/example/abc123/raw/h3.txt");
    await bridge.saveSettings(defaultSettings);await bridge.publishRun(h3.runId,"gist");settings.gist.h3={gistId:"def456",filename:"new.txt"};await bridge.savePublishSettings(request(settings));await vi.advanceTimersByTimeAsync(200);expect(published(h3.runId).url).toContain("abc123/raw/h3.txt");
    await bridge.publishRun(h3.runId,"gist");await vi.advanceTimersByTimeAsync(200);expect(published(h3.runId).url).toContain("def456/raw/new.txt");await bridge.publishRun(ordinary.runId,"gist");await vi.advanceTimersByTimeAsync(200);expect(published(ordinary.runId).url).toContain("abc123/raw/ip.txt");
    settings.gist.h3={gistId:"",filename:""};await bridge.savePublishSettings(request(settings));await bridge.publishRun(h3.runId,"gist");await vi.advanceTimersByTimeAsync(200);expect(published(h3.runId)).toMatchObject({state:"skipped",items:0});expect(published(h3.runId).url).toBeUndefined();expect(published(h3.runId).message).toContain("H3 未配置");
    await bridge.publishRun(ordinary.runId,"gist");await vi.advanceTimersByTimeAsync(200);expect(published(ordinary.runId).url).toContain("abc123/raw/ip.txt");expect((await bridge.bootstrap()).publishSettings.gist.tokenConfigured).toBe(true);off();offPublish();await bridge.savePublishSettings(request(defaultPublishSettings));
  });
  it("skips an automatic H3 publication when only the ordinary target is configured",async()=>{
    vi.useFakeTimers();const settings=targets();settings.gist.enabled=true;settings.gist.h3={gistId:"",filename:""};await bridge.savePublishSettings({...request(settings),gistToken:"shared-token"});await bridge.saveSettings({...defaultSettings,requireH3:true});
    const updates:PublicationUpdate[]=[];const off=bridge.onPublish(update=>updates.push(update));await bridge.startRun({startStage:"source",allowStaleCheckpoint:false});await vi.advanceTimersByTimeAsync(6000);
    expect(updates.filter(update=>update.result.target==="gist").map(update=>update.result.state)).toEqual(["skipped"]);expect(updates.some(update=>update.result.url)).toBe(false);off();await bridge.savePublishSettings(request(defaultPublishSettings));await bridge.saveSettings(defaultSettings);
  });
  it("preserves the actual H3 destination when copying a stable Raw URL",()=>{expect(latestGistRawURL("https://gist.githubusercontent.com/user/h3gist/raw/123456789abcdef/h3.txt")).toBe("https://gist.githubusercontent.com/user/h3gist/raw/h3.txt");});
  it("edits one mode at a time, keeps shared credentials and never changes the run mode",async()=>{
    await actions.init();await actions.saveSettings(defaultSettings);await actions.savePublishSettings({...request(defaultPublishSettings),gistToken:"shared-token"});render(<App/>);await screen.findByRole("heading",{name:"测速工作台"});fireEvent.click(screen.getByRole("button",{name:"发布"}));
    const modes=screen.getByRole("tablist",{name:"发布目标模式"});const gistCard=screen.getByRole("heading",{name:"GitHub Gist"}).closest("section")!;const test=vi.spyOn(actions,"testPublishTarget").mockResolvedValue();
    fireEvent.change(screen.getByLabelText("Gist ID",{exact:true}),{target:{value:"abc123"}});fireEvent.change(screen.getByLabelText("文件名",{exact:true}),{target:{value:"ip.txt"}});
    fireEvent.click(within(modes).getByRole("tab",{name:"H3"}));expect(screen.getByLabelText("Gist ID",{exact:true})).toHaveValue("");fireEvent.click(within(gistCard).getByRole("button",{name:"测试连接"}));await screen.findByText("H3 GitHub Gist 发布目标未配置");expect(test).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Gist ID",{exact:true}),{target:{value:"def456"}});fireEvent.change(screen.getByLabelText("文件名",{exact:true}),{target:{value:"h3.txt"}});expect(screen.getAllByLabelText("Gist Personal Access Token")).toHaveLength(1);
    fireEvent.click(within(gistCard).getByRole("button",{name:"测试连接"}));await waitFor(()=>expect(test).toHaveBeenCalledWith("gist",expect.objectContaining({testMode:"h3",gistToken:""})));
    fireEvent.click(within(modes).getByRole("tab",{name:"普通（TCP/HTTPS）"}));expect(screen.getByLabelText("Gist ID",{exact:true})).toHaveValue("abc123");fireEvent.click(within(modes).getByRole("tab",{name:"H3"}));expect(screen.getByLabelText("Gist ID",{exact:true})).toHaveValue("def456");expect(getAppState().settings.requireH3).toBe(false);
    fireEvent.click(screen.getByRole("button",{name:/保存设置/}));await waitFor(async()=>expect((await bridge.bootstrap()).publishSettings.gist.h3).toEqual({gistId:"def456",filename:"h3.txt"}));
    fireEvent.change(screen.getByLabelText("Gist ID",{exact:true}),{target:{value:""}});fireEvent.change(screen.getByLabelText("文件名",{exact:true}),{target:{value:""}});fireEvent.click(screen.getByRole("button",{name:/保存设置/}));await waitFor(async()=>expect((await bridge.bootstrap()).publishSettings.gist.h3).toEqual({gistId:"",filename:""}));
  });
});
