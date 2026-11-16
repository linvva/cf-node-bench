import { expect, test } from "@playwright/test";

for(const mode of ["tcp_https","h3"] as const){
  for(const keep of [true,false])test(`cancel choice ${keep?"keeps":"discards"} complete results in ${mode}`,async({page},testInfo)=>{
    await page.clock.install({time:new Date("2026-11-16T12:00:00Z")});
    await page.clock.pauseAt(new Date("2026-11-16T12:00:01Z"));
    await page.goto("/");
    await page.addStyleTag({content:"*, *::before, *::after { animation: none !important; transition: none !important; }"});
    await expect(page.getByRole("heading",{name:"测速工作台"})).toBeVisible();
    if(mode==="h3")await page.getByRole("tab",{name:"H3",exact:true}).click();
    await page.getByRole("button",{name:"开始测速",exact:true}).click();
    await page.clock.runFor(3120);
    await page.getByRole("button",{name:"取消测速",exact:true}).click();
    const dialog=page.getByRole("dialog");
    await expect(dialog).toContainText("选择期间测速仍会继续");
    await expect(dialog).toContainText("最终结果数选优");
    expect(await dialog.evaluate(element=>{
      const bounds=element.getBoundingClientRect();
      return [...element.querySelectorAll("button")].every(button=>{
        const rect=button.getBoundingClientRect();
        return rect.left>=bounds.left&&rect.right<=bounds.right&&rect.top>=bounds.top&&rect.bottom<=bounds.bottom;
      });
    })).toBe(true);
    await page.screenshot({path:testInfo.outputPath("cancel-choice.png")});
    await page.getByRole("button",{name:"继续测速",exact:true}).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole("button",{name:"取消测速",exact:true})).toBeVisible();
    await page.getByRole("button",{name:"取消测速",exact:true}).click();
    await page.getByRole("button",{name:keep?"保留已通过节点并结束":"丢弃本轮结果并取消",exact:true}).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole("button",{name:"开始测速",exact:true})).toBeVisible();
    if(keep){
      await expect(page.getByRole("heading",{name:"已取消，已保留部分结果",exact:true})).toBeVisible();
      await expect(page.getByRole("button",{name:"复制结果",exact:true})).toBeEnabled();
      await expect(page.getByRole("button",{name:"导出 CSV",exact:true})).toBeEnabled();
      await expect(page.getByRole("button",{name:"发布结果",exact:true})).toBeVisible();
      await expect(page.getByLabel("发布状态")).toContainText("尚无发布记录");
      expect(await page.locator(".status-pill").count()).toBeGreaterThan(0);
      const download=page.waitForEvent("download");
      await page.getByRole("button",{name:"导出 CSV",exact:true}).click();
      expect((await download).suggestedFilename()).toMatch(/\.csv$/);
    }else{
      await expect(page.getByRole("heading",{name:"测速已取消",exact:true})).toBeVisible();
      await expect(page.locator(".status-pill")).toHaveCount(0);
      await expect(page.getByRole("button",{name:"复制结果",exact:true})).toBeDisabled();
      await expect(page.getByRole("button",{name:"发布结果",exact:true})).toHaveCount(0);
    }
    await page.screenshot({path:testInfo.outputPath("cancelled-results.png")});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth-document.documentElement.clientWidth)).toBe(0);
  });

  test(`natural completion closes the cancel choice in ${mode}`,async({page})=>{
    await page.clock.install({time:new Date("2026-11-16T12:00:00Z")});
    await page.clock.pauseAt(new Date("2026-11-16T12:00:01Z"));
    await page.goto("/");
    await page.addStyleTag({content:"*, *::before, *::after { animation: none !important; transition: none !important; }"});
    await expect(page.getByRole("heading",{name:"测速工作台"})).toBeVisible();
    if(mode==="h3")await page.getByRole("tab",{name:"H3",exact:true}).click();
    await page.getByRole("button",{name:"开始测速",exact:true}).click();
    await page.clock.runFor(3120);
    await page.getByRole("button",{name:"取消测速",exact:true}).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.clock.runFor(1500);
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("heading",{name:"最近测速已完成",exact:true})).toBeVisible();
    await page.getByRole("button",{name:"开始测速",exact:true}).click();
    await expect(page.getByRole("button",{name:"取消测速",exact:true})).toBeEnabled();
    await expect(page.getByRole("dialog")).toHaveCount(0);
  });
}
