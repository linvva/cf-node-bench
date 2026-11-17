import { expect, test } from "@playwright/test";
import { installNative } from "./native";

test("desktop updates use generated RPC bindings and wait for active work before restart",async({page},testInfo)=>{
  await installNative(page);
  await page.addInitScript(()=>{
    const state={currentVersion:"0.9.0",status:"available",version:"0.10.0",notes:"改善测速体验",size:10485760};
    let downloaded=0,restarts=0;
    window.__nativeApi={...window.__nativeApi,
      UpdateStatus:()=>state,
      CheckForUpdate:()=>{state.status="available";window.__emitNative("update:state",state);return state;},
      DownloadUpdate:()=>{downloaded++;state.status="downloading";window.__emitNative("update:state",state);return state;},
      RestartForUpdate:()=>{restarts++;},
      TestUpdateCalls:()=>({downloaded,restarts}),
    };
  });
  await page.goto("/");
  await page.getByRole("button",{name:"查看应用更新"}).click();
  const download=page.getByRole("button",{name:"下载更新",exact:true});
  await expect(download).toBeVisible();
  expect(await page.evaluate(()=>window.__nativeApi.TestUpdateCalls())).toEqual({downloaded:0,restarts:0});
  await download.click();
  await page.evaluate(()=>window.__emitNative("update:state",{currentVersion:"0.9.0",status:"downloading",version:"0.10.0",size:10485760,downloaded:5242880}));
  await expect(page.getByRole("progressbar",{name:"更新下载进度"})).toHaveAttribute("value","5242880");
  await page.evaluate(()=>{
    window.__emitNative("run:progress",{runId:"active",state:"running",startedAt:new Date().toISOString(),stages:[],failures:{}});
    window.__emitNative("update:state",{currentVersion:"0.9.0",status:"ready",version:"0.10.0"});
  });
  const restart=page.getByRole("button",{name:"重启并更新",exact:true});
  await expect(restart).toBeDisabled();
  await expect(page.getByText("测速或发布任务结束后可重启更新",{exact:true})).toBeVisible();
  await page.screenshot({path:testInfo.outputPath("update-ready.png")});
  await page.evaluate(()=>window.__emitNative("run:complete",{runId:"active",state:"completed",startedAt:"",finishedAt:"",results:[],failures:{},publications:[],scoreWeights:{},reusedStages:[]}));
  await expect(restart).toBeEnabled();
  await restart.click();
  await expect.poll(()=>page.evaluate(()=>window.__nativeApi.TestUpdateCalls())).toEqual({downloaded:1,restarts:1});
});
