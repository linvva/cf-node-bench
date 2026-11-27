import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import { installNative } from "./native";

const documents=[
  {title:"项目许可证",path:"../../LICENSE"},
  {title:"第三方声明",path:"../public/THIRD_PARTY_NOTICES.md"},
  {title:"Unicode 许可",path:"../public/licenses/Unicode-3.0.txt"},
].map(document=>({...document,text:readFileSync(new URL(document.path,import.meta.url),"utf8")}));

for(const mode of ["preview","desktop"] as const)test(`license documents are available offline in ${mode}`,async({page,context},testInfo)=>{
  if(mode==="desktop")await installNative(page);
  await page.goto("/");
  await expect(page.getByRole("heading",{name:"测速工作台"})).toBeVisible();
  await page.evaluate(()=>document.fonts.ready);
  await context.setOffline(true);
  const requests:string[]=[];
  page.on("request",request=>requests.push(request.url()));
  await page.getByRole("button",{name:"设置",exact:true}).click();
  await page.getByRole("button",{name:"许可证与第三方声明",exact:true}).click();
  const dialog=page.getByRole("dialog",{name:"许可证与第三方声明"});
  await expect(dialog).toBeVisible();
  const tabRows=await dialog.getByRole("tab").evaluateAll(tabs=>tabs.map(tab=>Math.round(tab.getBoundingClientRect().top)));
  expect(new Set(tabRows).size).toBe(1);
  for(const document of documents){
    await dialog.getByRole("tab",{name:document.title,exact:true}).click();
    expect(await dialog.getByRole("tabpanel").locator("pre").textContent()).toBe(document.text);
  }
  await expect(dialog.getByRole("link")).toHaveCount(0);
  expect(requests).toEqual([]);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth-document.documentElement.clientWidth)).toBe(0);
  await page.screenshot({path:testInfo.outputPath(`license-${mode}.png`)});
  await dialog.getByRole("button",{name:"关闭许可证与第三方声明"}).click();
  await expect(dialog).toHaveCount(0);
  await page.getByRole("button",{name:"许可证与第三方声明",exact:true}).click();
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
});
