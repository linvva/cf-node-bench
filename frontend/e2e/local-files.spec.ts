import { expect, test } from "@playwright/test";
import type { Bootstrap, RunSummary, Settings } from "../src/types";

test("native file selection and window drops start one-time runs without changing sources",async({page},testInfo)=>{
  await page.addInitScript(()=>{
    const events:Record<string,(value:unknown)=>void>={};
    let dropped:((x:number,y:number,paths:string[])=>void)|undefined;
    const data={sources:[{id:"http",name:"原 HTTP 源",url:"https://example.test/nodes",enabled:true,nodeCount:12}],history:[],network:{interface:"test",ipv4:"",status:"online"},resume:{options:[]}} as unknown as Bootstrap;
    const state={selected:[] as string[],calls:[] as string[][],drop:(paths:string[])=>dropped?.(50,60,paths),complete:()=>{
      const summary={runId:`local-${state.calls.length}`,mode:data.settings?.requireH3?"h3":"tcp_https",localFiles:true,state:"completed",results:[],failures:{},publications:[],scoreWeights:data.settings?.scoreWeights,reusedStages:[],startedAt:new Date().toISOString(),finishedAt:new Date().toISOString()} as RunSummary;
      data.history.unshift(summary);data.currentResultRunId=summary.runId;events["run:complete"](summary);
    }};
    (window as typeof window&{__localFiles:typeof state}).__localFiles=state;
    window.go={main:{App:{Bootstrap:async()=>data,SelectLocalFiles:async()=>state.selected,StartLocalFiles:async(paths:string[])=>{state.calls.push(paths);return `local-${state.calls.length}`;},SaveSettings:async(settings:Settings)=>{data.settings=settings;}} as NonNullable<NonNullable<NonNullable<Window["go"]>["main"]>["App"]>}};
    window.runtime={EventsOn:(name,handler)=>{events[name]=handler;return ()=>{delete events[name];};},OnFileDrop:handler=>{dropped=handler;},OnFileDropOff:()=>{dropped=undefined;}};
  });
  await page.goto("/");
  const local=page.getByRole("button",{name:"本地文件测速",exact:true});
  await local.click();
  await expect(local).toBeEnabled();
  expect(await page.evaluate(()=>((window as unknown as {__localFiles:{calls:string[][]}}).__localFiles.calls))).toHaveLength(0);

  await page.evaluate(()=>{(window as unknown as {__localFiles:{selected:string[]}}).__localFiles.selected=["C:\\节点\\nodes.csv","C:\\Downloads\\nodes.json"];});
  await local.click();
  await expect(local).toBeDisabled();
  await expect(page.getByRole("tab",{name:"H3",exact:true})).toBeDisabled();
  await expect(page.locator(".stage").first()).toContainText("本地文件");
  await page.evaluate(()=>{(window as unknown as {__localFiles:{drop(paths:string[]):void}}).__localFiles.drop(["ignored.txt"]);});
  expect(await page.evaluate(()=>((window as unknown as {__localFiles:{calls:string[][]}}).__localFiles.calls))).toEqual([["C:\\节点\\nodes.csv","C:\\Downloads\\nodes.json"]]);
  await page.evaluate(()=>{(window as unknown as {__localFiles:{complete():void}}).__localFiles.complete();});
  await expect(page.getByRole("heading",{name:"测速结果 · TCP/HTTPS · 本地文件"})).toBeVisible();
  await expect(page.getByRole("combobox",{name:"查看历史运行"})).toContainText("本地文件");

  await page.getByRole("tab",{name:"H3",exact:true}).click();
  await expect(page.getByRole("tab",{name:"H3",exact:true})).toHaveAttribute("aria-selected","true");
  await page.getByRole("button",{name:"数据源",exact:true}).click();
  await expect(page.getByRole("heading",{name:"数据源管理"})).toBeVisible();
  await page.evaluate(()=>{const transfer=new DataTransfer();transfer.items.add(new File(["ignored browser content"],"drop.txt"));window.dispatchEvent(new DragEvent("dragenter",{dataTransfer:transfer,bubbles:true,cancelable:true}));});
  await expect(page.getByText("释放开始测速",{exact:true})).toBeVisible();
  await page.screenshot({path:testInfo.outputPath("local-file-drop.png")});
  await page.evaluate(()=>{const transfer=new DataTransfer();transfer.items.add(new File(["ignored browser content"],"drop.txt"));window.dispatchEvent(new DragEvent("drop",{dataTransfer:transfer,bubbles:true,cancelable:true}));(window as unknown as {__localFiles:{drop(paths:string[]):void}}).__localFiles.drop(["C:\\节点\\h3.txt"]);});
  await expect(page.getByRole("heading",{name:"测速工作台"})).toBeVisible();
  await expect(local).toBeDisabled();
  expect(await page.evaluate(()=>((window as unknown as {__localFiles:{calls:string[][]}}).__localFiles.calls))).toHaveLength(2);
  await page.evaluate(()=>{(window as unknown as {__localFiles:{complete():void}}).__localFiles.complete();});
  await expect(page.getByRole("heading",{name:"测速结果 · H3 · 本地文件"})).toBeVisible();
  await page.screenshot({path:testInfo.outputPath("local-file-results.png")});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth-document.documentElement.clientWidth)).toBe(0);
  await page.getByRole("button",{name:"数据源",exact:true}).click();
  await expect(page.getByText("原 HTTP 源",{exact:true})).toBeVisible();
  await expect(page.locator(".section-heading span")).toHaveText("1 / 1 已启用");
});
