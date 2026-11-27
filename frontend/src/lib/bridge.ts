import type { Bootstrap, DownloadCheck, Mode, PinnedNode, ProbeResult, Candidate, FailureReason, HTTPSource, PublicationResult, PublicationUpdate, PublishCredentialTarget, PublishSaveRequest, PublishSettingsView, PublishTarget, ResumeInfo, RunProgress, RunStartRequest, RunStartStage, RunSummary, ScoreWeights, Settings, StageProgress, UpdateState } from "../types";
import { DEFAULT_BANDWIDTH_TEST_URL, DEFAULT_H3_BANDWIDTH_TEST_URL } from "./bandwidthTestUrls";
import { DEFAULT_SCORE_WEIGHTS, H3_LEGACY_SCORE_WEIGHTS, LEGACY_SCORE_WEIGHTS, connectionWeight, httpWeight } from "./scoring";

type Handler<T> = (value: T) => void;

export function isDesktop(){
  const host=window as typeof window & {_wails?:{environment?:unknown};chrome?:{webview?:unknown};webkit?:{messageHandlers?:{external?:unknown}}};
  return Boolean(host._wails?.environment||host.chrome?.webview||host.webkit?.messageHandlers?.external||location.protocol==="wails:"||location.hostname==="wails.localhost");
}

let nativeModule:Promise<typeof import("./native")>|undefined;
function native(){return nativeModule??=import("./native");}
function nativeEvent<T>(name:string,handler:Handler<T>){
  let off:(()=>void)|undefined,disposed=false;
  if(isDesktop())void native().then(module=>{if(!disposed)off=module.onEvent(name,handler);}).catch(error=>console.error("桌面事件初始化失败",error));
  return ()=>{disposed=true;off?.();};
}

const progressHandlers = new Set<Handler<RunProgress>>();
const completeHandlers = new Set<Handler<RunSummary>>();
const publishHandlers = new Set<Handler<PublicationUpdate>>();
let mockTimer: number | undefined;
let mockActive=false;
let mockCancelled = false;
let mockRunId = "";
let mockStartedAt = "";
let mockCompleted = 0;
let mockQuick=false;
let mockStartStage:RunStartStage="source";
let mockParentRunId="";
let mockCheckpointSettings:Settings|undefined;
let mockRunSettings:Settings|undefined;

export const DEFAULT_VERIFICATION_HOST="h3.speed.cloudflare.com";
export const defaultSettings: Settings = {requireH3:false,verificationHost:"",h3ConnectTimeoutMs:3000,h3RequestTimeoutMs:6000,h3BandwidthTestUrl:DEFAULT_H3_BANDWIDTH_TEST_URL,connectionConcurrency:64,httpConcurrency:16,connectionProbeCount:3,httpProbeCount:3,connectionMinSuccessRate:2/3,httpMinSuccessRate:2/3,httpCandidateCount:150,bandwidthConcurrency:3,connectTimeoutMs:1200,requestTimeoutMs:4000,bandwidthTimeoutMs:12000,sourceTimeoutMs:10000,sourceRetries:2,bandwidthCandidates:30,finalResultCount:15,maxDownloadBytes:20971520,minBandwidthMbps:0,lineBandwidthMbps:0,bandwidthTestUrl:DEFAULT_BANDWIDTH_TEST_URL,retainPreviousResults:true,scoreWeights:DEFAULT_SCORE_WEIGHTS,allowedPorts:[443,8443,2053,2083,2087,2096],allowedCountries:[],blockedCountries:[]};
export const effectiveVerificationHost=(settings:Settings)=>settings.verificationHost.trim().toLowerCase().replace(/\.$/,"")||DEFAULT_VERIFICATION_HOST;
export const defaultPublishSettings: PublishSettingsView = {
  output:{format:"compact",country:true,tcpP95:false,httpLatency:true,bandwidth:true},
  request:{timeoutMs:10000,maxRetries:2,retryDelayMs:1000},
  cloudflare:{h3:{recordName:"",recordType:"TXT"},enabled:false,tokenConfigured:false,zoneId:"",recordName:"",recordType:"A",ttl:60,proxied:false},
  github:{h3:{path:""},enabled:false,tokenConfigured:false,owner:"",repository:"",branch:"main",path:""},
  gist:{h3:{gistId:"",filename:""},enabled:false,tokenConfigured:false,gistId:"",filename:""},
  telegram:{enabled:false,tokenConfigured:false,chatId:"",contentMode:"summary",deliveryMode:"direct",relayUrl:"",relayKeyConfigured:false},
};

export function normalizeSummary(value: RunSummary): RunSummary {
  return {
    ...value,
    mode:value?.mode??"tcp_https",
    results: Array.isArray(value?.results) ? value.results.map(result=>({...result,mode:result.mode??value.mode??"tcp_https"})) : [],
    failures: value?.failures ?? {},
    publications: Array.isArray(value?.publications) ? value.publications : [],
    scoreWeights:value?.scoreWeights&&Object.values(value.scoreWeights).some(Boolean)?value.scoreWeights:value.mode==="h3"?H3_LEGACY_SCORE_WEIGHTS:LEGACY_SCORE_WEIGHTS,
    reusedStages:Array.isArray(value?.reusedStages)?value.reusedStages:[],
  };
}

export function normalizeProgress(value: RunProgress): RunProgress {
  return {
    ...value,
    mode:value?.mode??"tcp_https",
    stages: Array.isArray(value?.stages) ? value.stages : [],
    failures: value?.failures ?? {},
  };
}

export function normalizeBootstrap(value: Bootstrap): Bootstrap {
  const settings = value?.settings ?? defaultSettings;
  const defaults=defaultSettings;
  const legacy=settings as Settings & {probeCount?:number;tcpProbeCount?:number;httpsProbeCount?:number};
  const weights=settings.scoreWeights??defaults.scoreWeights;
  return {
    ...value,
    version:value?.version||"dev",
    settings: {
      ...defaults,
      ...settings,
      connectionProbeCount:settings.connectionProbeCount??legacy.tcpProbeCount??legacy.probeCount??defaults.connectionProbeCount,
      httpProbeCount:settings.httpProbeCount??legacy.httpsProbeCount??legacy.probeCount??defaults.httpProbeCount,
      scoreWeights:{connection:connectionWeight(weights),http:httpWeight(weights),bandwidth:weights.bandwidth,reliability:weights.reliability,jitter:weights.jitter},
      allowedPorts: Array.isArray(settings.allowedPorts) ? settings.allowedPorts : [],
      allowedCountries: Array.isArray(settings.allowedCountries) ? settings.allowedCountries : [],
      blockedCountries: Array.isArray(settings.blockedCountries) ? settings.blockedCountries : [],
    },
    publishSettings: normalizePublishSettings(value?.publishSettings),
    sources: Array.isArray(value?.sources) ? value.sources : [],
    pinnedNodes: Array.isArray(value?.pinnedNodes) ? value.pinnedNodes : [],
    history: Array.isArray(value?.history) ? value.history.slice(0,40).map(normalizeSummary) : [],
    network: value?.network ?? {interface:"",ipv4:"",status:"unavailable"},
    resume:normalizeResume(value?.resume),
  };
}

function normalizeResume(value?:ResumeInfo):ResumeInfo{
  return {...value,options:Array.isArray(value?.options)?value.options.map(option=>({...option,warnings:option.warnings??[]})):[],continueBandwidth:value?.continueBandwidth?{...value.continueBandwidth,warnings:value.continueBandwidth.warnings??[]}:undefined};
}

let mockData: Bootstrap = {
  version:"dev",
  settings: defaultSettings,
  publishSettings: structuredClone(defaultPublishSettings),
  sources: [
    {id:"example-community-1",name:"社区示例源 A",url:"https://raw.githubusercontent.com/ymyuuu/IPDB/main/BestCF/bestcfv4.txt",enabled:true,lastStatus:"浏览器预览数据",nodeCount:48},
    {id:"example-community-2",name:"社区示例源 B",url:"https://ip.164746.xyz/ipTop10.html",enabled:false,lastStatus:"未获取",nodeCount:0},
  ],
  history: [], pinnedNodes: [], network:{interface:"浏览器预览",ipv4:"",status:"unavailable"}, resume:{options:[]},
};

const currentMockSettings=()=>mockActive&&mockRunSettings?mockRunSettings:mockData.settings;
const candidateKey=(candidate:Candidate)=>`${candidate.addressType==="domain"?candidate.host:candidate.ip}:${candidate.port}`;
const mockPinnedNodes=()=>(mockData.pinnedNodes??[]).filter(node=>node.mode===(currentMockSettings().requireH3?"h3":"tcp_https"));
const mockFreshCandidate=(candidate:Candidate)=>!mockPinnedNodes().some(node=>candidateKey(node.candidate)===candidateKey(candidate));
const startIndexFor=(plan:StageProgress[],stage:RunStartStage)=>stage==="source"?0:Math.max(0,plan.findIndex(item=>item.name===(stage==="quic"&&plan.some(item=>item.name==="dns")?"dns":stage)));
const mockDomain:Candidate={addressType:"domain",host:"edge.example.com",ip:"",port:443,country:"JP"};
const h3MockCandidates=():Candidate[]=>mockData.sources.some(source=>source.enabled)?[mockDomain,...mockCandidates].filter(mockFreshCandidate).filter(mockCandidateAllowed):[];
const mockDownloadPool=(available:number,scheduled:number)=>Math.min(available,Math.max(1,Math.ceil(scheduled/currentMockSettings().httpCandidateCount))*currentMockSettings().httpCandidateCount);
const mockTCPBandwidth=(index:number)=>186-index*8.5;
const mockH3Bandwidth=(index:number)=>160-index*4;
function mockBandwidthQualified(available:number, bandwidth:(index:number)=>number){
  const minimum=currentMockSettings().minBandwidthMbps;
  return minimum===0?available:Array.from({length:available},(_,index)=>bandwidth(index)).filter(mbps=>mbps>=minimum).length;
}
function h3MockPlan():StageProgress[]{
  const s=currentMockSettings(); const n=h3MockCandidates().length;
  const passed=Math.max(0,n-4);
  const bandwidth=Math.min(mockBandwidthQualified(passed,mockH3Bandwidth),s.bandwidthCandidates);
  const planned=bandwidth===s.bandwidthCandidates?bandwidth:passed;
  const selected=mockDownloadPool(passed,planned), final=Math.min(bandwidth,s.finalResultCount);
  const values:[string,number,number,number][]=[["source",mockData.sources.filter(source=>source.enabled).length,mockData.sources.filter(source=>source.enabled).length,0],["filter",n,n,0],["dns",n,n,0],["quic",n,passed,s.connectionProbeCount],["http3",passed,passed,s.httpProbeCount],["bandwidth",planned,bandwidth,1],["ranking",bandwidth,final,0]];
  const plan:StageProgress[]=values.map(([name,input,passed,attempts])=>({name,input,passed,failed:input-passed,attemptsCompleted:input*attempts,attemptsTotal:input*attempts,durationMs:input?850:0,state:"completed" as const,...(name==="http3"?{selected}:{}),...(name==="bandwidth"?{queue:{pool:selected,target:s.bandwidthCandidates,planned,active:0,pending:0,passed:bandwidth}}:{})}));
  const start=startIndexFor(plan,mockStartStage);
  return plan.map((stage,index)=>index<start?{...stage,state:"reused" as const,measuredAt:mockData.resume.updatedAt}:stage);
}
function h3MockResults():ProbeResult[]{
  const settings=currentMockSettings(),count=h3MockPlan().find(stage=>stage.name==="ranking")?.passed??0;
  const checkpoint=mockData.history.find(summary=>summary.runId===mockParentRunId);
  const reuseDNS=mockStartStage!=="source"&&mockStartStage!=="quic";
  const candidates=h3MockCandidates().slice(0,count);
  return candidates.map((candidate,index)=>{
    const quic={attempts:3,successes:3,successRate:1,averageMs:31+index,p50Ms:30+index,p95Ms:36+index,jitterMs:3,failures:{}};
    const http3={...quic,averageMs:55+index,p50Ms:53+index,p95Ms:60+index,jitterMs:4};
    const bandwidth={bytes:settings.maxDownloadBytes,ttfbMs:58+index,durationMs:1000,mbps:mockH3Bandwidth(index)};
    const ips=candidate.addressType==="domain"?["192.0.2.10","192.0.2.11"]:[candidate.ip];
    const empty={attempts:0,successes:0,successRate:0,averageMs:0,p50Ms:0,p95Ms:0,jitterMs:0,failures:{}};
    return {mode:"h3",candidate,tcp:empty,https:empty,quic,http3,bandwidth:{...bandwidth,bytes:bandwidth.bytes*ips.length},score:95-index*3,parts:{tcp:0,https:0,quic:95-index,http3:95-index,jitter:90,reliability:100,bandwidth:100-index*5},status:"qualified",resolvedAt:(reuseDNS?checkpoint?.results.find(result=>result.candidate.ip===candidate.ip&&result.candidate.host===candidate.host&&result.candidate.port===candidate.port)?.resolvedAt:undefined)??new Date().toISOString(),endpoints:ips.map(ip=>({ip,quic,http3,bandwidth}))};
  });
}

const mockCountries=["CN","US","JP","HK","SG","DE"];
const mockCandidates=Array.from({length:48},(_,index)=>({addressType:"ipv4" as const,ip:`104.18.${Math.floor(index/250)+1}.${index+20}`,port:index%4===0?8443:443,country:mockCountries[index%mockCountries.length],sourceId:"example-community-1"}));

function mockCandidateAllowed(candidate: Candidate) {
  const settings=currentMockSettings();
  const country=candidate.country??"";
  return (settings.allowedPorts.length===0||settings.allowedPorts.includes(candidate.port))
    && !settings.blockedCountries.includes(country)
    && (settings.allowedCountries.length===0||settings.allowedCountries.includes(country));
}

function mockPlan(): StageProgress[] {
  if(currentMockSettings().requireH3)return h3MockPlan();
  const settings=currentMockSettings();
  const latestCompleted=mockData.history.find(summary=>summary.state==="completed"&&(summary.mode??"tcp_https")==="tcp_https");
  const retained=settings.retainPreviousResults?(latestCompleted?.results??[]).filter(result=>result.candidate.addressType==="ipv4"&&mockFreshCandidate(result.candidate)):[];
  const retainedKeys=new Set(retained.map(result=>`${result.candidate.ip}:${result.candidate.port}`));
  const freshCandidates=mockCandidates.filter(mockFreshCandidate).filter(candidate=>!retainedKeys.has(`${candidate.ip}:${candidate.port}`));
  const freshByKey=new Map(mockCandidates.map(candidate=>[`${candidate.ip}:${candidate.port}`,candidate]));
  const portPassed=freshCandidates.filter(candidate=>settings.allowedPorts.length===0||settings.allowedPorts.includes(candidate.port));
  const filtered=freshCandidates.filter(mockCandidateAllowed);
  const portFailed=freshCandidates.length-portPassed.length;
  const countryFailed=portPassed.filter(candidate=>!mockCandidateAllowed(candidate)).length;
  const tcpFailed=Math.min(4,filtered.length); const tcpPassed=filtered.length-tcpFailed;
  const httpsInput=tcpPassed,httpsFailed=Math.min(2,httpsInput),httpsPassed=httpsInput-httpsFailed;
  const seenRetained=new Set<string>();
  const retainedInput=retained.filter(result=>{const key=`${result.candidate.ip}:${result.candidate.port}`;if(seenRetained.has(key))return false;seenRetained.add(key);return mockCandidateAllowed(freshByKey.get(key)??result.candidate);}).length;
  const retainedFailed=Math.min(1,retainedInput); const retainedPassed=retainedInput-retainedFailed;
  const target=settings.bandwidthCandidates;
  const freshPassed=Math.min(mockBandwidthQualified(Math.max(0,httpsPassed-1),mockTCPBandwidth),target);
  const freshInput=freshPassed===target?Math.min(httpsPassed,target+1):httpsPassed;
  const selected=mockDownloadPool(httpsPassed,freshInput);
  const bandwidthInput=freshInput+retainedPassed;
  const bandwidthPassed=mockBandwidthQualified(freshPassed+retainedPassed,mockTCPBandwidth);
  const rankingPassed=Math.min(bandwidthPassed,settings.finalResultCount);
  const plan:StageProgress[]=[
    {name:"source",input:mockData.sources.filter(source=>source.enabled).length,passed:mockData.sources.filter(source=>source.enabled).length,failed:0,durationMs:180,state:"completed"},
    {name:"filter",input:freshCandidates.length+2,passed:filtered.length,failed:2+portFailed+countryFailed,durationMs:24,state:"completed"},
    {name:"tcp",input:filtered.length,passed:tcpPassed,failed:tcpFailed,attemptsCompleted:filtered.length*settings.connectionProbeCount,attemptsTotal:filtered.length*settings.connectionProbeCount,durationMs:1480,state:"completed"},
    {name:"https",input:httpsInput,passed:httpsPassed,failed:httpsFailed,selected,attemptsCompleted:httpsInput*settings.httpProbeCount,attemptsTotal:httpsInput*settings.httpProbeCount,durationMs:1760,state:"completed"},
    {name:"retained",input:retainedInput,passed:retainedPassed,failed:retainedFailed,attemptsCompleted:retainedInput,attemptsTotal:retainedInput,durationMs:retainedInput?420:0,state:"completed"},
    {name:"bandwidth",input:bandwidthInput,passed:bandwidthPassed,failed:bandwidthInput-bandwidthPassed,queue:{pool:selected,target,planned:freshInput,active:0,pending:0,passed:freshPassed},attemptsCompleted:bandwidthInput,attemptsTotal:bandwidthInput,durationMs:1930,state:"completed"},
    {name:"ranking",input:bandwidthInput,passed:rankingPassed,failed:bandwidthInput-rankingPassed,durationMs:28,state:"completed"},
  ];
  const startIndex=startIndexFor(plan,mockStartStage);
  return plan.map((stage,index)=>{
    if(index>=startIndex)return stage;
    if(stage.name==="retained"&&mockStartStage==="https")return {...stage,input:0,passed:0,failed:0,state:"skipped"};
    return {...stage,state:"reused",measuredAt:new Date(Date.now()-60_000).toISOString()};
  });
}

function mockRunningStage(stage: StageProgress): StageProgress {
  const ratio=0.42;
  const planned=Math.ceil((stage.queue?.planned??0)*ratio);
  const queue=stage.queue?{...stage.queue,pool:mockDownloadPool(stage.queue.pool,planned),planned,passed:Math.floor(stage.queue.passed*ratio),active:Math.min(currentMockSettings().bandwidthConcurrency,Math.max(0,planned-Math.floor(stage.queue.passed*ratio)-Math.floor(stage.failed*ratio)))}:undefined;
  if(queue)queue.pending=Math.max(0,queue.planned-queue.active-queue.passed-Math.floor(stage.failed*ratio));
  return {
    ...stage,
    selected:undefined,
    queue,
    passed:Math.floor(stage.passed*ratio),
    failed:Math.floor(stage.failed*ratio),
    attemptsCompleted:stage.attemptsTotal?Math.max(1,Math.floor(stage.attemptsTotal*ratio)):undefined,
    durationMs:Math.max(80,Math.floor(stage.durationMs*ratio)),
    state:"running",
  };
}

function mockProgressStages(plan:StageProgress[],completed:number):StageProgress[]{
  const stages=completed===plan.length?plan.map(stage=>({...stage})):plan.slice(0,completed+1).map((stage,index)=>index<completed?{...stage}:mockRunningStage(stage));
  const http=stages.find(stage=>stage.name==="https"||stage.name==="http3");
  if(http?.selected!==undefined){
    const download=stages.find(stage=>stage.name==="bandwidth");
    http.selected=download?.queue?.pool??Math.min(http.selected,currentMockSettings().httpCandidateCount);
  }
  return stages;
}

function mockFailures(plan: StageProgress[], completed: number): Partial<Record<FailureReason,number>> {
  const failures:Partial<Record<FailureReason,number>>={};
  const download=plan.slice(0,completed).find(stage=>stage.name==="bandwidth");
  if(currentMockSettings().requireH3){
    for(const stage of plan.slice(0,completed)){if(stage.name==="quic")failures.quic_timeout=stage.failed;}
    if(download?.failed)failures.bandwidth_below_minimum=download.failed;
    return failures;
  }
  if(completed>1){
    const settings=currentMockSettings();
    const retained=settings.retainPreviousResults?(mockData.history.find(summary=>summary.state==="completed"&&(summary.mode??"tcp_https")==="tcp_https")?.results??[]).filter(result=>result.candidate.addressType==="ipv4"&&mockFreshCandidate(result.candidate)):[];
    const retainedKeys=new Set(retained.map(result=>candidateKey(result.candidate)));
    const fresh=mockCandidates.filter(mockFreshCandidate).filter(candidate=>!retainedKeys.has(candidateKey(candidate)));
    failures.invalid_ip=2;
    const portFiltered=fresh.filter(candidate=>settings.allowedPorts.length>0&&!settings.allowedPorts.includes(candidate.port)).length;
    if(portFiltered)failures.port_filtered=portFiltered;
    const countryFiltered=plan[1].failed-2-portFiltered;
    if(countryFiltered)failures.country_filtered=countryFiltered;
  }
  if(completed>2&&plan[2].failed)failures.timeout=plan[2].failed;
  if(completed>3&&plan[3].failed)failures.tls=plan[3].failed;
  if(completed>4&&plan[4].failed)failures.tls=(failures.tls??0)+plan[4].failed;
  if(download?.failed){
    const otherFailures=download.queue?.planned?1:0;
    if(otherFailures)failures.download=otherFailures;
    if(download.failed>otherFailures)failures.bandwidth_below_minimum=download.failed-otherFailures;
  }
  return failures;
}

function mockResultsWithPinned(results:ProbeResult[]):ProbeResult[]{
  const settings=currentMockSettings();
  const pinned=mockPinnedNodes().map(node=>{
    const empty={attempts:0,successes:0,successRate:0,averageMs:0,p50Ms:0,p95Ms:0,jitterMs:0,failures:{}};
    const previous:ProbeResult=mockData.history.filter(summary=>(summary.mode??"tcp_https")===node.mode).flatMap(summary=>summary.results).find(result=>candidateKey(result.candidate)===candidateKey(node.candidate))??{
      candidate:node.candidate,tcp:empty,https:empty,quic:empty,http3:empty,bandwidth:{bytes:0,ttfbMs:0,durationMs:0,mbps:0},score:0,parts:{tcp:0,https:0,jitter:0,reliability:0,bandwidth:0},status:"failed_gate",
    };
    const result={...structuredClone(previous),candidate:structuredClone(node.candidate),mode:node.mode,pinned:true};
    if(node.mode==="h3")result.resolvedAt=new Date().toISOString();
    for(const stats of [result.bandwidth,...(result.endpoints??[]).map(endpoint=>endpoint.bandwidth)]){
      if(stats.mbps>0&&stats.mbps<settings.minBandwidthMbps&&!stats.failure)stats.failure="bandwidth_below_minimum";
    }
    const connection=node.mode==="h3"?result.quic:result.tcp,http=node.mode==="h3"?result.http3:result.https;
    const passed=(connection?.successRate??0)>=settings.connectionMinSuccessRate&&(http?.successRate??0)>=settings.httpMinSuccessRate&&result.bandwidth.mbps>0&&!result.bandwidth.failure;
    result.status=passed?"qualified":"failed_gate";
    if(!passed){result.score=0;result.parts={tcp:0,https:0,quic:0,http3:0,jitter:0,reliability:0,bandwidth:0};}
    return result;
  });
  return [...results,...pinned];
}

function mockAvailableResults(plan:StageProgress[],completed:number){
  const passed=mockProgressStages(plan,completed).find(stage=>stage.name==="bandwidth")?.passed??0;
  const pinned=completed>5?mockResultsWithPinned([]).filter(result=>result.status==="qualified").length:0;
  return passed+pinned;
}

function mockResults(runId=mockRunId, startedAt=mockStartedAt, completed=mockPlan().length, keepResults=false): RunSummary {
  const plan=mockPlan();
  const results=mockCandidates.filter(mockFreshCandidate).filter(mockCandidateAllowed).slice(0,plan[6].passed).map((candidate,index)=>({
    candidate,
    tcp:{attempts:3,successes:3,successRate:1,averageMs:18+index*2.1,p50Ms:17+index*2,p95Ms:22+index*2.4,jitterMs:2+index*.2,failures:{}},
    https:{attempts:3,successes:index===8?2:3,successRate:index===8?2/3:1,averageMs:44+index*3,p50Ms:42+index*3,p95Ms:51+index*3.4,jitterMs:3.2+index*.35,failures:index===8?{timeout:1}:{}},
    bandwidth:{bytes:20971520,ttfbMs:39+index*2,durationMs:900+index*85,mbps:mockTCPBandwidth(index)},
    score:96.2-index*3.7,parts:{tcp:98-index*2,https:96-index*2.4,jitter:94-index*2,reliability:index===8?83:100,bandwidth:100-index*5},status:"qualified",
  }));
  const passed=mockProgressStages(plan,completed).find(stage=>stage.name==="bandwidth")?.passed??0;
  const measured=(currentMockSettings().requireH3?h3MockResults():results).slice(0,passed);
  const retained=mockCancelled&&!keepResults?[]:completed>5?mockResultsWithPinned(measured):measured;
  const now=new Date(); return {runId,startedAt:startedAt||now.toISOString(),finishedAt:now.toISOString(),quick:mockQuick,stages:mockProgressStages(plan,completed).map(stage=>mockCancelled&&stage.state==="running"?{...stage,state:"cancelled"}:stage),mode:currentMockSettings().requireH3?"h3":"tcp_https",workerHost:currentMockSettings().requireH3?effectiveVerificationHost(currentMockSettings()):undefined,logicalCandidates:currentMockSettings().requireH3?h3MockCandidates().length:undefined,physicalCandidates:currentMockSettings().requireH3?h3MockCandidates().reduce((sum,candidate)=>sum+(candidate.addressType==="domain"?2:1),0):undefined,downloadedBytes:currentMockSettings().requireH3&&completed>5&&mockStartStage!=="ranking"?(plan[5].passed+(plan[5].passed>0&&h3MockCandidates().some(candidate=>candidate.addressType==="domain")?1:0))*currentMockSettings().maxDownloadBytes:0,state:mockCancelled?"cancelled":"completed",partialResults:mockCancelled&&keepResults&&retained.length>0,results:retained,failures:mockFailures(plan,completed),publications:[],scoreWeights:structuredClone(currentMockSettings().scoreWeights),parentRunId:mockParentRunId||undefined,startStage:mockStartStage,reusedStages:plan.filter(stage=>stage.state==="reused").map(stage=>stage.name)};
}

function mockResume(summary:RunSummary):ResumeInfo{
  const settings=mockData.settings,checkpoint=mockCheckpointSettings??settings;
  const h3=settings.requireH3,connection=h3?"quic":"tcp",http=h3?"http3":"https";
  const changed=checkpoint.requireH3!==h3||effectiveVerificationHost(checkpoint)!==effectiveVerificationHost(settings);
  const connectionChanged=(h3?checkpoint.h3ConnectTimeoutMs!==settings.h3ConnectTimeoutMs:checkpoint.connectTimeoutMs!==settings.connectTimeoutMs)||checkpoint.connectionProbeCount!==settings.connectionProbeCount||checkpoint.connectionMinSuccessRate!==settings.connectionMinSuccessRate||JSON.stringify([checkpoint.allowedPorts,checkpoint.allowedCountries,checkpoint.blockedCountries])!==JSON.stringify([settings.allowedPorts,settings.allowedCountries,settings.blockedCountries]);
  const httpChanged=(h3?checkpoint.h3RequestTimeoutMs!==settings.h3RequestTimeoutMs:checkpoint.requestTimeoutMs!==settings.requestTimeoutMs)||checkpoint.httpProbeCount!==settings.httpProbeCount||checkpoint.httpMinSuccessRate!==settings.httpMinSuccessRate;
  const bandwidthChanged=checkpoint.bandwidthTimeoutMs!==settings.bandwidthTimeoutMs||checkpoint.maxDownloadBytes!==settings.maxDownloadBytes||checkpoint.bandwidthCandidates!==settings.bandwidthCandidates||(h3?checkpoint.h3BandwidthTestUrl!==settings.h3BandwidthTestUrl:checkpoint.bandwidthTestUrl!==settings.bandwidthTestUrl);
  const thresholdChanged=checkpoint.minBandwidthMbps!==settings.minBandwidthMbps||(settings.minBandwidthMbps>0&&checkpoint.lineBandwidthMbps!==settings.lineBandwidthMbps);
  const warnings=(stage:RunStartStage)=>[stage!==connection&&connectionChanged?"连接测量或候选过滤设置已变化":"",(stage==="bandwidth"||stage==="ranking")&&httpChanged?"HTTP 测量设置已变化":"",stage==="ranking"&&bandwidthChanged?"带宽测量设置已变化":"",stage==="ranking"&&thresholdChanged?"带宽门槛或线路上限已变化，请从带宽阶段重测":""].filter(Boolean);
  const measuredAt=summary.finishedAt;
  const names=[connection,http,"bandwidth","ranking"] as const;
  const plan=mockPlan();
  const completed=summary.state==="completed"?plan.length:mockCompleted;
  return {baseRunId:summary.runId,updatedAt:measuredAt,options:names.map(stage=>({stage,input:plan.find(item=>item.name===stage)?.input??0,available:startIndexFor(plan,stage)<=completed&&(!changed||stage===connection)&&!(stage==="ranking"&&thresholdChanged),measuredAt,warnings:changed&&stage!==connection?["H3 要求或验证域名已变化，需要重新探测连接"]:warnings(stage)}))};
}

function normalizePublishSettings(value?:PublishSettingsView):PublishSettingsView {
  return {
    output:{...defaultPublishSettings.output,...value?.output,format:value?.output?.format||"compact"},
    request:{...defaultPublishSettings.request,...value?.request},
    cloudflare:{...defaultPublishSettings.cloudflare,...value?.cloudflare,h3:{recordType:value?.cloudflare?.h3?.recordType||"TXT",recordName:value?.cloudflare?.h3?.recordName?.trim().toLowerCase().replace(/\.$/,"")??""},recordType:value?.cloudflare?.recordType||"A"},
    github:{...defaultPublishSettings.github,...value?.github,h3:{path:value?.github?.h3?.path?.trim()??""},path:value?.github?.path?.trim()??""},
    gist:{...defaultPublishSettings.gist,...value?.gist,h3:{gistId:value?.gist?.h3?.gistId?.trim()??"",filename:value?.gist?.h3?.filename?.trim()??""},gistId:value?.gist?.gistId?.trim()??"",filename:value?.gist?.filename?.trim()??""},
    telegram:{...defaultPublishSettings.telegram,...value?.telegram,contentMode:value?.telegram?.contentMode||"summary",deliveryMode:value?.telegram?.deliveryMode||"direct"},
  };
}

// Resolve a value snapshot from the result's protocol, independently of the next run.
export function publishSettingsForMode(settings:PublishSettingsView,mode?:Mode):PublishSettingsView {
  const value=structuredClone(settings);
  if(mode!=="h3")return value;
  value.cloudflare.recordName=value.cloudflare.h3.recordName;value.cloudflare.recordType=value.cloudflare.h3.recordType;
  value.github.path=value.github.h3.path;
  value.gist.gistId=value.gist.h3.gistId;value.gist.filename=value.gist.h3.filename;
  return value;
}

export function publishTargetConfigured(settings:PublishSettingsView,target:PublishTarget,mode?:Mode):boolean {
  if(target==="telegram")return Boolean(settings.telegram.chatId.trim());
  if(target==="cloudflare")return Boolean((mode==="h3"?settings.cloudflare.h3.recordName:settings.cloudflare.recordName).trim());
  if(target==="github")return Boolean((mode==="h3"?settings.github.h3.path:settings.github.path).trim());
  const gist=mode==="h3"?settings.gist.h3:settings.gist;
  return Boolean(gist.gistId.trim()&&gist.filename.trim());
}

function mockPublication(update:PublicationUpdate){
  mockData.history=mockData.history.map(summary=>summary.runId===update.runId?{...summary,publications:[...summary.publications.filter(value=>value.target!==update.result.target),update.result]}:summary);
  publishHandlers.forEach(handler=>handler(update));
}

const previewUpdate:UpdateState={currentVersion:"dev",status:"unavailable",error:"浏览器预览不支持自动更新，请使用桌面应用"};

export const bridge = {
  async updateStatus():Promise<UpdateState>{return isDesktop()?(await native()).app.UpdateStatus():{...previewUpdate};},
  async checkForUpdate():Promise<UpdateState>{return isDesktop()?(await native()).app.CheckForUpdate():{...previewUpdate};},
  async downloadUpdate():Promise<UpdateState>{if(!isDesktop())throw new Error(previewUpdate.error);return (await native()).app.DownloadUpdate();},
  async restartForUpdate(){if(!isDesktop())throw new Error(previewUpdate.error);await (await native()).app.RestartForUpdate();},
  onUpdate(handler:Handler<UpdateState>){return nativeEvent<UpdateState>("update:state",handler);},
  async openURL(url:string){if(isDesktop())await (await native()).openURL(url);else window.open(url,"_blank","noopener,noreferrer");},
  async bootstrap(): Promise<Bootstrap> { const value=isDesktop() ? await (await native()).app.Bootstrap() : structuredClone(mockData); return normalizeBootstrap(value); },
  async saveSettings(value: Settings) { if(isDesktop()) return (await native()).app.SaveSettings(value); if(mockActive&&value.requireH3!==mockData.settings.requireH3)throw new Error("测速中不能切换 H3 要求"); mockData.settings=structuredClone(value); const base=mockData.history.find(summary=>summary.runId===mockData.resume.baseRunId); if(base)mockData.resume=mockResume(base); },
  async saveSources(value: HTTPSource[]) { if(isDesktop()) return (await native()).app.SaveSources(value); mockData.sources=structuredClone(value); },
  async setNodePinned(runId:string,key:string,pinned:boolean):Promise<PinnedNode[]>{
    if(isDesktop())return (await (await native()).app.SetNodePinned(runId,key,pinned)??[]) as PinnedNode[];
    if(mockActive)throw new Error("测速运行中不能修改固化节点");
    const summary=mockData.history.find(item=>item.runId===runId);
    const result=summary?.results.find(item=>candidateKey(item.candidate)===key);
    if(!summary||!result)throw new Error("未找到测速结果中的节点");
    const mode=summary.mode??"tcp_https";
    const nodes=(mockData.pinnedNodes??[]).filter(node=>node.mode!==mode||candidateKey(node.candidate)!==key);
    if(pinned)nodes.push({mode,candidate:structuredClone(result.candidate)});
    mockData.pinnedNodes=nodes;
    return structuredClone(nodes);
  },
  async savePublishSettings(value: PublishSaveRequest) {
    if(isDesktop()) return (await native()).app.SavePublishSettings(value);
    const current=mockData.publishSettings;
    const next=normalizePublishSettings(value.settings);
    mockData.publishSettings={...structuredClone(next),cloudflare:{...next.cloudflare,tokenConfigured:current.cloudflare.tokenConfigured||Boolean(value.cloudflareToken.trim())},github:{...next.github,tokenConfigured:current.github.tokenConfigured||Boolean(value.githubToken.trim())},gist:{...next.gist,tokenConfigured:current.gist.tokenConfigured||Boolean(value.gistToken.trim())},telegram:{...next.telegram,tokenConfigured:current.telegram.tokenConfigured||Boolean(value.telegramBotToken.trim()),relayKeyConfigured:current.telegram.relayKeyConfigured||Boolean(value.telegramRelayKey.trim())}};
    return structuredClone(mockData.publishSettings);
  },
  async clearPublishCredential(target: PublishCredentialTarget) {
    if(isDesktop()) return (await native()).app.ClearPublishCredential(target);
    if(target==="telegramRelay"){
      const telegram=mockData.publishSettings.telegram;
      mockData.publishSettings={...mockData.publishSettings,telegram:{...telegram,enabled:telegram.deliveryMode==="relay"?false:telegram.enabled,relayKeyConfigured:false}};
    }else{
      mockData.publishSettings={...mockData.publishSettings,[target]:{...mockData.publishSettings[target],enabled:false,tokenConfigured:false}};
    }
    return structuredClone(mockData.publishSettings);
  },
  async testPublishTarget(target: PublishTarget,value:PublishSaveRequest) { if(isDesktop()) return (await native()).app.TestPublishTarget(target,value); },
  async publishRun(runId:string,target:"all"|PublishTarget) {
    if(isDesktop()) return (await native()).app.PublishRun(runId,target);
    const summary=mockData.history.find(item=>item.runId===runId);
    const publishSettings=publishSettingsForMode(mockData.publishSettings,summary?.mode);
    if(!summary||(summary.state!=="completed"&&!(summary.state==="cancelled"&&summary.partialResults&&summary.results.length))) throw new Error("只有已完成或已保留部分结果的测速可以发布");
    const targets:PublishTarget[]=target==="all"?(["cloudflare","github","gist","telegram"] as PublishTarget[]).filter(current=>publishSettings[current].enabled):[target];
    const configured=targets.filter(current=>publishSettings[current].enabled&&publishTargetConfigured(publishSettings,current));
    for(const current of targets) mockPublication({runId,result:configured.includes(current)?{target:current,state:"queued",items:0}:{target:current,state:"skipped",items:0,message:publishSettings[current].enabled?`${summary.mode==="h3"?"H3":"普通（TCP/HTTPS）"} 未配置发布目标`:"渠道未启用"}});
    window.setTimeout(()=>{
      const now=new Date().toISOString();
      for(const current of configured){
        const aRecords=current==="cloudflare"&&publishSettings.cloudflare.recordType==="A";
        const eligible=aRecords?summary.results.filter(result=>result.candidate.addressType==="ipv4"&&result.candidate.port===443).length:summary.results.length;
        const skipped=summary.results.length-eligible;
        mockPublication({runId,result:{target:current,state:eligible?"succeeded":"skipped",items:eligible,eligible,skipped,recordType:current==="cloudflare"?publishSettings.cloudflare.recordType:undefined,url:current==="gist"?`https://gist.githubusercontent.com/example/${encodeURIComponent(publishSettings.gist.gistId)}/raw/${encodeURIComponent(publishSettings.gist.filename)}`:undefined,message:current==="cloudflare"?`已写入 ${publishSettings.cloudflare.recordName} 的 ${eligible} 条记录；跳过 ${skipped} 个域名或非 443 端口候选`:current==="github"?`已发布 ${publishSettings.github.path}`:"发布成功",startedAt:now,finishedAt:now}});
      }
    },180);
  },
  async selectLocalFiles():Promise<string[] | null> {
    if(isDesktop()) return (await native()).app.SelectLocalFiles();
    throw new Error("浏览器预览不支持读取本地文件，请使用桌面应用");
  },
  async testDownloadAddress(settings:Settings):Promise<DownloadCheck> {
    if(isDesktop()) return (await native()).app.TestDownloadAddress(settings);
    throw new Error("浏览器预览无法通过候选节点发起请求，请使用桌面应用");
  },
  async startLocalFiles(paths:string[], quick=false):Promise<string> {
    if(isDesktop()){
      const {app}=await native();
      return quick?app.StartLocalFilesWithStrategy(paths,true):app.StartLocalFiles(paths);
    }
    throw new Error("浏览器预览不支持读取本地文件，请使用桌面应用");
  },
  onFileDrop(handler:(paths:string[])=>void){
    return nativeEvent<string[]>("files:dropped",handler);
  },
  async startRun(request:RunStartRequest) {
    if(isDesktop()) return (await native()).app.StartRun(request);
    if(mockActive)throw new Error("已有测速任务运行中");
    if(request.startStage!=="source"){
      const option=request.continueBandwidth?mockData.resume.continueBandwidth:mockData.resume.options.find(item=>item.stage===request.startStage);
      if(!option?.available||request.baseRunId!==mockData.resume.baseRunId)throw new Error("阶段检查点已变化，请刷新后重试");
      if(option.warnings.length&&!request.allowStaleCheckpoint)throw new Error(`阶段检查点需要确认：${option.warnings.join("；")}`);
    }
    mockActive=true;mockCancelled=false; mockParentRunId=request.startStage==="source"?"":mockData.resume.baseRunId??""; mockStartStage=request.startStage; mockQuick=request.startStage==="source"?Boolean(request.quick):Boolean(mockData.history.find(item=>item.runId===request.baseRunId)?.quick); mockRunId=`run-${Date.now()}`; mockStartedAt=new Date().toISOString(); mockRunSettings=structuredClone(mockData.settings); const plan=mockPlan(); const startIndex=startIndexFor(plan,mockStartStage); mockCompleted=startIndex; let step=startIndex;
    mockTimer=window.setInterval(()=>{ const completed=Math.min(step,plan.length); mockCompleted=completed; const stages=mockProgressStages(plan,completed); progressHandlers.forEach(h=>h({runId:mockRunId,quick:mockQuick,mode:currentMockSettings().requireH3?"h3":"tcp_https",logicalCandidates:currentMockSettings().requireH3?h3MockCandidates().length:undefined,physicalCandidates:currentMockSettings().requireH3?h3MockCandidates().reduce((sum,candidate)=>sum+(candidate.addressType==="domain"?2:1),0):undefined,downloadedBytes:currentMockSettings().requireH3&&completed>5&&mockStartStage!=="ranking"?(plan[5].passed+(plan[5].passed>0&&h3MockCandidates().some(candidate=>candidate.addressType==="domain")?1:0))*currentMockSettings().maxDownloadBytes:0,state:"running",availableResults:mockAvailableResults(plan,completed),startedAt:mockStartedAt,stages,failures:mockFailures(plan,completed)})); if(completed===plan.length){ window.clearInterval(mockTimer); const summary=mockResults(); mockData.history=[summary,...mockData.history].slice(0,40); mockData.currentResultRunId=summary.runId; mockCheckpointSettings=mockRunSettings; mockData.resume=mockResume(summary); mockActive=false;completeHandlers.forEach(h=>h(summary)); if(summary.state==="completed")void bridge.publishRun(summary.runId,"all"); } step++; },520); return mockRunId;
  },
  async cancelRun(keepResults=false,saveBandwidth=false){ if(isDesktop()){const {app}=await native();return saveBandwidth?app.SaveBandwidthAndStop():keepResults?app.CancelRunWithResults(true):app.CancelRun();} if(!mockActive)return false; mockCancelled=true; window.clearInterval(mockTimer); const summary=mockResults(mockRunId,mockStartedAt,mockCompleted,keepResults); mockData.history=[summary,...mockData.history].slice(0,40); mockData.currentResultRunId=summary.runId; mockCheckpointSettings=mockRunSettings; mockData.resume=mockResume(summary); mockActive=false;completeHandlers.forEach(h=>h(summary)); return true; },
  onProgress(handler:Handler<RunProgress>){ progressHandlers.add(handler); const off=nativeEvent<RunProgress>("run:progress",value=>handler(normalizeProgress(value))); return ()=>{progressHandlers.delete(handler); off?.();}; },
  onComplete(handler:Handler<RunSummary>){ completeHandlers.add(handler); const off=nativeEvent<RunSummary>("run:complete",value=>handler(normalizeSummary(value))); return ()=>{completeHandlers.delete(handler); off?.();}; },
  onPublish(handler:Handler<PublicationUpdate>){ publishHandlers.add(handler); const off=nativeEvent<PublicationUpdate>("publish:progress",handler); return ()=>{publishHandlers.delete(handler); off?.();}; },
};
