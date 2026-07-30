import { expect, test } from "@playwright/test";

test("workspace remains stable through a complete run",async({page},testInfo)=>{
  await page.addInitScript(()=>{
    Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:async(value:string)=>{(window as typeof window&{__copiedText?:string}).__copiedText=value;}}});
  });
  await page.goto("/");
  await expect(page.getByRole("heading",{name:"测速工作台"})).toBeVisible();
  const stage=page.getByLabel("测速阶段");
  const before=await stage.boundingBox();
  await page.getByRole("button",{name:/开始测速/}).click();
  await expect(page.getByRole("button",{name:/取消测速/})).toBeVisible();
  const during=await stage.boundingBox();
  expect(during?.height).toBe(before?.height);
  await expect(page.getByText("104.18.1.20:8443")).toBeVisible({timeout:8000});
  await page.getByText("104.18.1.20:8443").click();
  await expect(page.locator('[data-slot="drawer-content"]')).toBeVisible();
  await page.waitForTimeout(350);
  expect(await page.locator(".drawer__dialog").evaluate(element=>element.getBoundingClientRect().width)).toBeGreaterThan(350);
  await page.screenshot({path:testInfo.outputPath("drawer.png"),fullPage:true});
  await page.getByRole("button",{name:"关闭"}).click();
  await expect(page.locator('[data-slot="drawer-content"]')).not.toBeVisible();
  await page.getByLabel("复制选项").click();
  await expect(page.getByRole("checkbox",{name:"国家代码"})).toBeChecked();
  await expect(page.getByRole("checkbox",{name:"HTTP 平均延迟"})).toBeChecked();
  await expect(page.getByRole("checkbox",{name:"下载带宽"})).toBeChecked();
  await page.locator('[data-slot="checkbox-content"]').filter({hasText:"TCP P95"}).click();
  await page.getByRole("button",{name:"复制结果"}).click();
  expect(await page.evaluate(()=>(window as typeof window&{__copiedText?:string}).__copiedText?.split("\n")[0])).toBe("104.18.1.20:8443#CN|TCP22ms|HTTP44ms|186Mbps");
  await page.screenshot({path:testInfo.outputPath("workspace.png"),fullPage:true});
  const overflow=await page.evaluate(()=>({x:document.documentElement.scrollWidth-document.documentElement.clientWidth,y:document.documentElement.scrollHeight-document.documentElement.clientHeight}));
  expect(overflow.x).toBe(0); expect(overflow.y).toBe(0);
});

test("progress stays responsive while previous results remain visible",async({page},testInfo)=>{
  await page.goto("/");
  await page.getByRole("button",{name:/开始测速/}).click();
  await expect(page.getByText("104.18.1.20:8443")).toBeVisible({timeout:8000});

  await page.getByRole("button",{name:/开始测速/}).click();
  await expect(page.getByRole("button",{name:/取消测速/})).toBeVisible();
  const tcpStage=page.locator(".stage").filter({hasText:"TCP"});
  await expect(tcpStage.locator(".stage-time")).toContainText("探测",{timeout:4000});
  await page.screenshot({path:testInfo.outputPath("progress.png"),fullPage:true});

  const content=page.locator(".main-content");
  await content.evaluate(element=>{element.scrollTop=element.scrollHeight;});
  expect(await content.evaluate(element=>element.scrollTop)).toBeGreaterThan(0);
  await page.waitForTimeout(700);
  await content.evaluate(element=>{element.scrollTop=0;});
  expect(await content.evaluate(element=>element.scrollTop)).toBe(0);
  await page.getByRole("button",{name:/取消测速/}).click();
});

test("settings and sources fit without overlap",async({page},testInfo)=>{
  await page.goto("/");
  await page.getByRole("button",{name:"设置"}).click();
  await expect(page.getByRole("heading",{name:"测速设置"})).toBeVisible();
  await page.getByLabel("允许国家选择器").click();
  await page.getByRole("searchbox",{name:"搜索允许国家"}).fill("中国");
  await page.getByRole("option",{name:/中国.*CN/}).click();
  await expect(page.getByLabel("允许国家已选")).toContainText("CN");
  const countryFlag=page.getByLabel("允许国家已选").locator(".country-flag").first();
  await expect(countryFlag).toBeVisible();
  expect(await countryFlag.evaluate(element=>getComputedStyle(element).fontFamily)).toContain("Twemoji Mozilla");
  expect(await page.evaluate(()=>document.fonts.check('16px "Twemoji Mozilla"',"🇨🇳"))).toBe(true);
  await expect(page.getByRole("searchbox",{name:"搜索允许国家"})).not.toBeVisible();
  await page.getByLabel("排除国家选择器").click();
  await page.getByRole("searchbox",{name:"搜索排除国家"}).fill("CN");
  await page.getByRole("option",{name:/中国.*CN/}).click();
  await expect(page.getByRole("searchbox",{name:"搜索排除国家"})).not.toBeVisible();
  await page.screenshot({path:testInfo.outputPath("settings.png"),fullPage:true});
  const settingsClipped=await page.locator("button, input").evaluateAll(nodes=>nodes.filter(node=>node.scrollWidth>node.clientWidth+1||node.scrollHeight>node.clientHeight+1).length);
  expect(settingsClipped).toBe(0);
  await page.locator(".main-content").evaluate(element=>{element.scrollTop=element.scrollHeight;});
  await expect(page.getByRole("button",{name:/保存设置/})).toBeVisible();
  await page.getByRole("button",{name:/保存设置/}).click();
  const countryError=page.locator(".settings-footer .settings-error");
  await expect(countryError).toHaveText("国家 CN 不能同时出现在允许和排除列表");
  await page.locator(".main-content").evaluate(element=>{element.scrollTop=element.scrollHeight;});
  await expect(countryError).toBeVisible();
  await page.screenshot({path:testInfo.outputPath("settings-bottom.png"),fullPage:true});
  await page.getByRole("button",{name:"数据源"}).click();
  await expect(page.getByRole("heading",{name:"数据源管理"})).toBeVisible();
  await page.screenshot({path:testInfo.outputPath("sources.png"),fullPage:true});
  const clipped=await page.locator("button, input").evaluateAll(nodes=>nodes.filter(node=>node.scrollWidth>node.clientWidth+1||node.scrollHeight>node.clientHeight+1).length);
  expect(clipped).toBe(0);
});

test("source enable switches update the active count",async({page})=>{
  await page.goto("/");
  await page.getByRole("button",{name:"数据源"}).click();
  const firstSource=page.getByRole("switch",{name:"社区示例源 A 启用状态"});
  const secondSource=page.getByRole("switch",{name:"社区示例源 B 启用状态"});
  const switches=page.locator('[data-slot="switch-content"]');
  await expect(firstSource).toBeChecked();
  await expect(secondSource).not.toBeChecked();
  await switches.nth(0).click();
  await expect(firstSource).not.toBeChecked();
  await expect(page.locator(".section-heading span")).toHaveText("0 / 2 已启用");
  await switches.nth(1).click();
  await expect(secondSource).toBeChecked();
  await expect(page.locator(".section-heading span")).toHaveText("1 / 2 已启用");
});

test("publish settings switch modes without overlap",async({page},testInfo)=>{
  await page.goto("/");
  await page.getByRole("button",{name:"发布"}).click();
  await expect(page.getByRole("heading",{name:"结果发布"})).toBeVisible();
  await expect(page.getByText("启用 Cloudflare 代理")).toBeVisible();
  await page.getByRole("tab",{name:"TXT 记录"}).click();
  await expect(page.getByText("启用 Cloudflare 代理")).not.toBeVisible();
  await expect(page.getByText("104.18.1.20:443#US|HTTP44ms|186Mbps")).toBeVisible();
  await page.locator(".main-content").evaluate(element=>{element.scrollTop=0;});
  await page.screenshot({path:testInfo.outputPath("publish-top.png"),fullPage:true});
  await page.getByRole("tab",{name:"汇总与节点列表"}).click();
  await page.locator(".main-content").evaluate(element=>{element.scrollTop=element.scrollHeight;});
  await expect(page.getByRole("button",{name:/测试连接/}).last()).toBeVisible();
  await page.screenshot({path:testInfo.outputPath("publish.png"),fullPage:true});
  const clipped=await page.locator("button, input, code").evaluateAll(nodes=>nodes.filter(node=>node.scrollWidth>node.clientWidth+1||node.scrollHeight>node.clientHeight+1).length);
  expect(clipped).toBe(0);
  const overlaps=await page.evaluate(()=>{
    const visible=[...document.querySelectorAll(".publish-section button, .publish-section input")].filter(element=>{const rect=element.getBoundingClientRect();return rect.width>0&&rect.height>0;});
    return visible.some((element,index)=>visible.slice(index+1).some(other=>{const a=element.getBoundingClientRect(),b=other.getBoundingClientRect();return a.left<b.right&&a.right>b.left&&a.top<b.bottom&&a.bottom>b.top;}));
  });
  expect(overlaps).toBe(false);
});

test("blocked countries change filter totals and results",async({page})=>{
  await page.goto("/");
  await page.getByRole("button",{name:"设置"}).click();
  await page.getByLabel("排除国家选择器").click();
  await page.getByRole("searchbox",{name:"搜索排除国家"}).fill("CN");
  await page.getByRole("option",{name:/中国.*CN/}).click();
  await page.locator(".main-content").evaluate(element=>{element.scrollTop=element.scrollHeight;});
  await page.getByRole("button",{name:/保存设置/}).click();
  await page.getByRole("button",{name:"测速工作台"}).click();
  await page.getByRole("button",{name:/开始测速/}).click();
  const filterStage=page.locator(".stage").filter({hasText:"解析 / 过滤"});
  await expect(filterStage).toHaveAttribute("data-state","completed",{timeout:8000});
  expect(await filterStage.locator(".stage-metrics strong").allTextContents()).toEqual(["50","40","10"]);
  await expect(page.getByText("104.18.1.21:443")).toBeVisible({timeout:8000});
  await expect(page.locator(".table__cell").filter({hasText:/^CN$/})).toHaveCount(0);
});
