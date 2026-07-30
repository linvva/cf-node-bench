import type { Bootstrap, FailureReason, HTTPSource, PublicationResult, PublicationUpdate, PublishCredentialTarget, PublishSaveRequest, PublishSettingsView, PublishTarget, RunProgress, RunSummary, Settings, StageProgress } from "../types";

type Handler<T> = (value: T) => void;

declare global {
  interface Window {
    go?: { main?: { App?: { Bootstrap(): Promise<Bootstrap>; SaveSettings(value: Settings): Promise<void>; SaveSources(value: HTTPSource[]): Promise<void>; SavePublishSettings(value: PublishSaveRequest): Promise<PublishSettingsView>; ClearPublishCredential(target: PublishCredentialTarget): Promise<PublishSettingsView>; TestPublishTarget(target: PublishTarget, value: PublishSaveRequest): Promise<void>; PublishRun(runId: string, target: "all" | PublishTarget): Promise<void>; StartRun(): Promise<string>; CancelRun(): Promise<boolean> } } };
    runtime?: { EventsOn(name: string, handler: Handler<unknown>): () => void; BrowserOpenURL?(url: string): void };
  }
}

const progressHandlers = new Set<Handler<RunProgress>>();
const completeHandlers = new Set<Handler<RunSummary>>();
const publishHandlers = new Set<Handler<PublicationUpdate>>();
let mockTimer: number | undefined;
let mockCancelled = false;
let mockRunId = "";
let mockStartedAt = "";
let mockCompleted = 0;

const defaultSettings: Settings = { tcpConcurrency:64, httpsConcurrency:16, bandwidthConcurrency:3, connectTimeoutMs:1200, requestTimeoutMs:4000, bandwidthTimeoutMs:12000, sourceTimeoutMs:10000, sourceRetries:2, tcpProbeCount:3, httpsProbeCount:3, tcpMinSuccessRate:2/3, httpsMinSuccessRate:2/3, tcpCandidateCount:150, bandwidthCandidates:30, finalResultCount:15, maxDownloadBytes:20971520, allowedPorts:[443,8443,2053,2083,2087,2096], allowedCountries:[], blockedCountries:[] };
export const defaultPublishSettings: PublishSettingsView = {
  output:{country:true,tcpP95:false,httpLatency:true,bandwidth:true},
  request:{timeoutMs:10000,maxRetries:2,retryDelayMs:1000},
  cloudflare:{enabled:false,tokenConfigured:false,zoneId:"",recordName:"",recordType:"A",ttl:60,proxied:false},
  github:{enabled:false,tokenConfigured:false,owner:"",repository:"",branch:"main",path:"ip.txt"},
  gist:{enabled:false,tokenConfigured:false,gistId:"",filename:"ip.txt"},
  telegram:{enabled:false,tokenConfigured:false,chatId:"",contentMode:"summary",deliveryMode:"direct",relayUrl:"",relayKeyConfigured:false},
};

export function normalizeSummary(value: RunSummary): RunSummary {
  return {
    ...value,
    results: Array.isArray(value?.results) ? value.results : [],
    failures: value?.failures ?? {},
    publications: Array.isArray(value?.publications) ? value.publications : [],
  };
}

export function normalizeProgress(value: RunProgress): RunProgress {
  return {
    ...value,
    stages: Array.isArray(value?.stages) ? value.stages : [],
    failures: value?.failures ?? {},
  };
}

export function normalizeBootstrap(value: Bootstrap): Bootstrap {
  const settings = value?.settings ?? defaultSettings;
  const legacyProbeCount = (settings as Settings & {probeCount?:number}).probeCount;
  return {
    ...value,
    settings: {
      ...defaultSettings,
      ...settings,
      tcpProbeCount: settings.tcpProbeCount || legacyProbeCount || defaultSettings.tcpProbeCount,
      httpsProbeCount: settings.httpsProbeCount || legacyProbeCount || defaultSettings.httpsProbeCount,
      allowedPorts: Array.isArray(settings.allowedPorts) ? settings.allowedPorts : [],
      allowedCountries: Array.isArray(settings.allowedCountries) ? settings.allowedCountries : [],
      blockedCountries: Array.isArray(settings.blockedCountries) ? settings.blockedCountries : [],
    },
    publishSettings: normalizePublishSettings(value?.publishSettings),
    sources: Array.isArray(value?.sources) ? value.sources : [],
    history: Array.isArray(value?.history) ? value.history.map(normalizeSummary) : [],
    network: value?.network ?? {interface:"",ipv4:"",status:"unavailable"},
  };
}

let mockData: Bootstrap = {
  settings: defaultSettings,
  publishSettings: structuredClone(defaultPublishSettings),
  sources: [
    {id:"example-community-1",name:"社区示例源 A",url:"https://raw.githubusercontent.com/ymyuuu/IPDB/main/BestCF/bestcfv4.txt",enabled:true,lastStatus:"浏览器预览数据",nodeCount:48},
    {id:"example-community-2",name:"社区示例源 B",url:"https://ip.164746.xyz/ipTop10.html",enabled:false,lastStatus:"未获取",nodeCount:0},
  ],
  history: [], network:{interface:"浏览器预览",ipv4:"",status:"unavailable"},
};

const mockCountries=["CN","US","JP","HK","SG","DE"];
const mockCandidates=Array.from({length:48},(_,index)=>({addressType:"ipv4" as const,ip:`104.18.${Math.floor(index/250)+1}.${index+20}`,port:index%4===0?8443:443,country:mockCountries[index%mockCountries.length],sourceId:"example-community-1"}));

function mockCandidateAllowed(candidate: typeof mockCandidates[number]) {
  const settings=mockData.settings;
  return (settings.allowedPorts.length===0||settings.allowedPorts.includes(candidate.port))
    && !settings.blockedCountries.includes(candidate.country)
    && (settings.allowedCountries.length===0||settings.allowedCountries.includes(candidate.country));
}

function mockPlan(): StageProgress[] {
  const settings=mockData.settings;
  const portPassed=mockCandidates.filter(candidate=>settings.allowedPorts.length===0||settings.allowedPorts.includes(candidate.port));
  const filtered=mockCandidates.filter(mockCandidateAllowed);
  const portFailed=mockCandidates.length-portPassed.length;
  const countryFailed=portPassed.filter(candidate=>!mockCandidateAllowed(candidate)).length;
  const tcpFailed=Math.min(4,filtered.length); const tcpPassed=filtered.length-tcpFailed;
  const httpsInput=Math.min(tcpPassed,settings.tcpCandidateCount); const httpsFailed=Math.min(2,httpsInput); const httpsPassed=httpsInput-httpsFailed;
  const bandwidthInput=Math.min(httpsPassed,settings.bandwidthCandidates); const bandwidthFailed=Math.min(1,bandwidthInput);
  const rankingPassed=Math.min(bandwidthInput,settings.finalResultCount);
  return [
    {name:"source",input:mockData.sources.filter(source=>source.enabled).length,passed:mockData.sources.filter(source=>source.enabled).length,failed:0,durationMs:180,state:"completed"},
    {name:"filter",input:mockCandidates.length+2,passed:filtered.length,failed:2+portFailed+countryFailed,durationMs:24,state:"completed"},
    {name:"tcp",input:filtered.length,passed:tcpPassed,failed:tcpFailed,attemptsCompleted:filtered.length*settings.tcpProbeCount,attemptsTotal:filtered.length*settings.tcpProbeCount,durationMs:1480,state:"completed"},
    {name:"https",input:httpsInput,passed:httpsPassed,failed:httpsFailed,attemptsCompleted:httpsInput*settings.httpsProbeCount,attemptsTotal:httpsInput*settings.httpsProbeCount,durationMs:1760,state:"completed"},
    {name:"bandwidth",input:bandwidthInput,passed:bandwidthInput-bandwidthFailed,failed:bandwidthFailed,attemptsCompleted:bandwidthInput,attemptsTotal:bandwidthInput,durationMs:1930,state:"completed"},
    {name:"ranking",input:bandwidthInput,passed:rankingPassed,failed:bandwidthInput-rankingPassed,durationMs:28,state:"completed"},
  ];
}

function mockRunningStage(stage: StageProgress): StageProgress {
  const ratio=0.42;
  return {
    ...stage,
    passed:Math.floor(stage.passed*ratio),
    failed:Math.floor(stage.failed*ratio),
    attemptsCompleted:stage.attemptsTotal?Math.max(1,Math.floor(stage.attemptsTotal*ratio)):undefined,
    durationMs:Math.max(80,Math.floor(stage.durationMs*ratio)),
    state:"running",
  };
}

function mockFailures(plan: StageProgress[], completed: number): Partial<Record<FailureReason,number>> {
  const failures:Partial<Record<FailureReason,number>>={};
  if(completed>1){ failures.invalid_ip=2; const portFiltered=plan[1].input-2-mockCandidates.filter(candidate=>mockData.settings.allowedPorts.length===0||mockData.settings.allowedPorts.includes(candidate.port)).length; if(portFiltered)failures.port_filtered=portFiltered; const countryFiltered=plan[1].failed-2-portFiltered; if(countryFiltered)failures.country_filtered=countryFiltered; }
  if(completed>2&&plan[2].failed)failures.timeout=plan[2].failed;
  if(completed>3&&plan[3].failed)failures.tls=plan[3].failed;
  if(completed>4&&plan[4].failed)failures.download=plan[4].failed;
  return failures;
}

function mockResults(runId=mockRunId, startedAt=mockStartedAt, completed=mockPlan().length): RunSummary {
  const plan=mockPlan();
  const results=mockCandidates.filter(mockCandidateAllowed).slice(0,plan[5].passed).map((candidate,index)=>({
    candidate,
    tcp:{attempts:3,successes:3,successRate:1,averageMs:18+index*2.1,p50Ms:17+index*2,p95Ms:22+index*2.4,jitterMs:2+index*.2,failures:{}},
    https:{attempts:3,successes:index===8?2:3,successRate:index===8?2/3:1,averageMs:44+index*3,p50Ms:42+index*3,p95Ms:51+index*3.4,jitterMs:3.2+index*.35,failures:index===8?{timeout:1}:{}},
    bandwidth:{bytes:20971520,ttfbMs:39+index*2,durationMs:900+index*85,mbps:186-index*8.5},
    score:96.2-index*3.7,parts:{tcp:98-index*2,https:96-index*2.4,jitter:94-index*2,reliability:index===8?83:100,bandwidth:100-index*5},status:"qualified",
  }));
  const now=new Date(); return {runId,startedAt:startedAt||now.toISOString(),finishedAt:now.toISOString(),state:mockCancelled?"cancelled":"completed",results:mockCancelled?[]:results,failures:mockFailures(plan,completed),publications:[]};
}

function normalizePublishSettings(value?:PublishSettingsView):PublishSettingsView {
  return {
    output:{...defaultPublishSettings.output,...value?.output},
    request:{...defaultPublishSettings.request,...value?.request},
    cloudflare:{...defaultPublishSettings.cloudflare,...value?.cloudflare,recordType:value?.cloudflare?.recordType||"A"},
    github:{...defaultPublishSettings.github,...value?.github},
    gist:{...defaultPublishSettings.gist,...value?.gist},
    telegram:{...defaultPublishSettings.telegram,...value?.telegram,contentMode:value?.telegram?.contentMode||"summary",deliveryMode:value?.telegram?.deliveryMode||"direct"},
  };
}

function mockPublication(update:PublicationUpdate){
  mockData.history=mockData.history.map(summary=>summary.runId===update.runId?{...summary,publications:[...summary.publications.filter(value=>value.target!==update.result.target),update.result]}:summary);
  publishHandlers.forEach(handler=>handler(update));
}

export const bridge = {
  async bootstrap(): Promise<Bootstrap> { const value=window.go?.main?.App ? await window.go.main.App.Bootstrap() : structuredClone(mockData); return normalizeBootstrap(value); },
  async saveSettings(value: Settings) { if(window.go?.main?.App) return window.go.main.App.SaveSettings(value); mockData.settings=structuredClone(value); },
  async saveSources(value: HTTPSource[]) { if(window.go?.main?.App) return window.go.main.App.SaveSources(value); mockData.sources=structuredClone(value); },
  async savePublishSettings(value: PublishSaveRequest) {
    if(window.go?.main?.App) return window.go.main.App.SavePublishSettings(value);
    const current=mockData.publishSettings;
    mockData.publishSettings={...structuredClone(value.settings),cloudflare:{...value.settings.cloudflare,tokenConfigured:current.cloudflare.tokenConfigured||Boolean(value.cloudflareToken.trim())},github:{...value.settings.github,tokenConfigured:current.github.tokenConfigured||Boolean(value.githubToken.trim())},gist:{...value.settings.gist,tokenConfigured:current.gist.tokenConfigured||Boolean(value.gistToken.trim())},telegram:{...value.settings.telegram,tokenConfigured:current.telegram.tokenConfigured||Boolean(value.telegramBotToken.trim()),relayKeyConfigured:current.telegram.relayKeyConfigured||Boolean(value.telegramRelayKey.trim())}};
    return structuredClone(mockData.publishSettings);
  },
  async clearPublishCredential(target: PublishCredentialTarget) {
    if(window.go?.main?.App) return window.go.main.App.ClearPublishCredential(target);
    if(target==="telegramRelay"){
      const telegram=mockData.publishSettings.telegram;
      mockData.publishSettings={...mockData.publishSettings,telegram:{...telegram,enabled:telegram.deliveryMode==="relay"?false:telegram.enabled,relayKeyConfigured:false}};
    }else{
      mockData.publishSettings={...mockData.publishSettings,[target]:{...mockData.publishSettings[target],enabled:false,tokenConfigured:false}};
    }
    return structuredClone(mockData.publishSettings);
  },
  async testPublishTarget(target: PublishTarget,value:PublishSaveRequest) { if(window.go?.main?.App) return window.go.main.App.TestPublishTarget(target,value); },
  async publishRun(runId:string,target:"all"|PublishTarget) {
    if(window.go?.main?.App) return window.go.main.App.PublishRun(runId,target);
    const summary=mockData.history.find(item=>item.runId===runId);
    if(!summary||summary.state!=="completed") throw new Error("只有已完成的测速可以发布");
    const targets:PublishTarget[]=target==="all"?(["cloudflare","github","gist","telegram"] as PublishTarget[]).filter(current=>mockData.publishSettings[current].enabled):[target];
    for(const current of targets) mockPublication({runId,result:{target:current,state:"queued",items:0}});
    window.setTimeout(()=>{
      const now=new Date().toISOString();
      for(const current of targets) mockPublication({runId,result:{target:current,state:"succeeded",items:summary.results.length,recordType:current==="cloudflare"?mockData.publishSettings.cloudflare.recordType:undefined,url:current==="gist"?"https://gist.githubusercontent.com/example/raw/ip.txt":undefined,message:current==="cloudflare"?`已写入 ${summary.results.length} 条记录`:"发布成功",startedAt:now,finishedAt:now}});
    },180);
  },
  async startRun() {
    if(window.go?.main?.App) return window.go.main.App.StartRun();
    mockCancelled=false; mockRunId=`run-${Date.now()}`; mockStartedAt=new Date().toISOString(); mockCompleted=0; const plan=mockPlan(); let step=0;
    mockTimer=window.setInterval(()=>{ const completed=Math.min(step,plan.length); mockCompleted=completed; const stages=completed===plan.length?plan:plan.slice(0,completed+1).map((stage,index)=>index<completed?stage:mockRunningStage(stage)); progressHandlers.forEach(h=>h({runId:mockRunId,state:"running",startedAt:mockStartedAt,stages,failures:mockFailures(plan,completed)})); if(completed===plan.length){ window.clearInterval(mockTimer); const summary=mockResults(); mockData.history=[summary,...mockData.history]; completeHandlers.forEach(h=>h(summary)); if(summary.state==="completed")void bridge.publishRun(summary.runId,"all"); } step++; },520); return mockRunId;
  },
  async cancelRun(){ if(window.go?.main?.App) return window.go.main.App.CancelRun(); mockCancelled=true; window.clearInterval(mockTimer); const summary=mockResults(mockRunId,mockStartedAt,mockCompleted); completeHandlers.forEach(h=>h(summary)); return true; },
  onProgress(handler:Handler<RunProgress>){ progressHandlers.add(handler); const off=window.runtime?.EventsOn("run:progress",value=>handler(normalizeProgress(value as RunProgress))); return ()=>{progressHandlers.delete(handler); off?.();}; },
  onComplete(handler:Handler<RunSummary>){ completeHandlers.add(handler); const off=window.runtime?.EventsOn("run:complete",value=>handler(normalizeSummary(value as RunSummary))); return ()=>{completeHandlers.delete(handler); off?.();}; },
  onPublish(handler:Handler<PublicationUpdate>){ publishHandlers.add(handler); const off=window.runtime?.EventsOn("publish:progress",value=>handler(value as PublicationUpdate)); return ()=>{publishHandlers.delete(handler); off?.();}; },
};
