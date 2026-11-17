import { useSyncExternalStore } from "react";
import { bridge } from "./lib/bridge";
import type { Bootstrap, HTTPSource, PublicationResult, PublicationUpdate, PublishCredentialTarget, PublishSaveRequest, PublishSettingsView, PublishTarget, RunProgress, RunStartRequest, RunSummary, Settings, UpdateState } from "./types";

interface AppState extends Bootstrap { ready:boolean; running:boolean; cancelling:boolean; preparingLocal:boolean; savingMode:boolean; progress?:RunProgress; current?:RunSummary; error?:string; startupError?:string; update:UpdateState; restartingUpdate:boolean; publishing:boolean }
const initial:AppState={version:"dev",update:{currentVersion:"dev",status:"idle"},restartingUpdate:false,publishing:false,ready:false,running:false,cancelling:false,preparingLocal:false,savingMode:false,settings:{} as Settings,publishSettings:{} as PublishSettingsView,sources:[],history:[],pinnedNodes:[],network:{interface:"",ipv4:"",status:"unavailable"},resume:{options:[]}};
let state=initial;
let cancelRequestPending=false;
let updateInitialized=false;
let updateRevision=0;
const activePublications=new Set<string>();
const listeners=new Set<()=>void>();
const setState=(patch:Partial<AppState>)=>{state={...state,...patch};listeners.forEach(listener=>listener());};

export const actions={
  async init(){
    try {
      const data=await bridge.bootstrap();
      setState({...data,ready:true,startupError:undefined,current:data.history.find(summary=>summary.runId===data.currentResultRunId)??data.history[0]});
      if(!updateInitialized){updateInitialized=true;void updateAction(()=>bridge.updateStatus());}
    } catch(error){setState({ready:true,startupError:String(error)});}
  },
  async checkForUpdate(){await updateAction(()=>bridge.checkForUpdate());},
  async downloadUpdate(){await updateAction(()=>bridge.downloadUpdate());},
  async restartForUpdate(){
    if(updateBusy(state))throw new Error("请等待测速和发布任务结束后重启");
    setState({restartingUpdate:true,update:{...state.update,error:undefined}});
    try{await bridge.restartForUpdate();}catch(error){setState({restartingUpdate:false,update:{...state.update,error:String(error)}});}
  },
  async start(request:RunStartRequest={startStage:"source",allowStaleCheckpoint:false}){
    if(state.running||state.cancelling||state.preparingLocal||state.savingMode||state.restartingUpdate)throw new Error("已有测速任务正在运行或准备中");
    const input=request.startStage==="source"?state.sources.filter(source=>source.enabled).length:state.resume.options.find(option=>option.stage===request.startStage)?.input??0;
    const localFiles=request.startStage!=="source"&&Boolean(state.current?.localFiles);
    return startTask(()=>bridge.startRun(request),request.startStage,input,localFiles);
  },
  async startLocal(paths?:string[]){
    if(state.running||state.cancelling||state.preparingLocal||state.savingMode||state.restartingUpdate)throw new Error("已有测速任务正在运行或准备中");
    setState({preparingLocal:true,error:undefined});
    try {
      const selected=paths??await bridge.selectLocalFiles();
      if(!selected?.length)return;
      return await startTask(()=>bridge.startLocalFiles(selected),"source",1,true);
    } finally {setState({preparingLocal:false});}
  },
  async cancel(keepResults=false,runId=state.progress?.runId||state.currentRunId){
    if(!state.running||state.cancelling||!runId||runId!==(state.progress?.runId||state.currentRunId))return false;
    cancelRequestPending=true;setState({cancelling:true});
    try{const accepted=await bridge.cancelRun(keepResults);if(!accepted||!state.running)setState({cancelling:false});return accepted;}catch(error){setState({cancelling:false});throw error;}finally{cancelRequestPending=false;}
  },
  async saveSettings(settings:Settings){ await bridge.saveSettings(settings); const data=await bridge.bootstrap(); setState({settings:data.settings,resume:data.resume}); },
  async saveSources(sources:HTTPSource[]){ await bridge.saveSources(sources); setState({sources}); },
  async setNodePinned(runId:string,candidateKey:string,pinned:boolean){
    if(state.running||state.cancelling||state.preparingLocal||state.savingMode||state.restartingUpdate)throw new Error("测速运行中或准备中不能修改固化节点");
    const pinnedNodes=await bridge.setNodePinned(runId,candidateKey,pinned);
    setState({pinnedNodes});
    const data=await bridge.bootstrap();
    setState({resume:data.resume});
  },
  async setRequireH3(requireH3:boolean){if(state.running||state.cancelling||state.preparingLocal||state.savingMode||state.restartingUpdate)throw new Error("测速中或准备中不能切换模式");setState({savingMode:true});try{await actions.saveSettings({...state.settings,requireH3});}finally{setState({savingMode:false});}},
  async savePublishSettings(request:PublishSaveRequest){const publishSettings=await bridge.savePublishSettings(request);setState({publishSettings});return publishSettings;},
  async clearPublishCredential(target:PublishCredentialTarget){const publishSettings=await bridge.clearPublishCredential(target);setState({publishSettings});return publishSettings;},
  async testPublishTarget(target:PublishTarget,request:PublishSaveRequest){await bridge.testPublishTarget(target,request);},
  async publishRun(runId:string,target:"all"|PublishTarget){ await bridge.publishRun(runId,target); },
};

async function startTask(start:()=>Promise<string>,stage:RunStartRequest["startStage"],input:number,localFiles:boolean){
  setState({running:true,cancelling:false,error:undefined,progress:{runId:"",mode:state.settings.requireH3?"h3":"tcp_https",localFiles,state:"running",startedAt:new Date().toISOString(),stages:[{name:stage,input,passed:0,failed:0,durationMs:0,state:"running"}],failures:{}}});
  try {
    const id=await start();
    if(state.running)setState({currentRunId:id,progress:state.progress?{...state.progress,runId:state.progress.runId||id}:undefined});
    return id;
  } catch(error){setState({running:false,progress:undefined,error:String(error)});throw error;}
}

async function updateAction(run:()=>Promise<UpdateState>){
  const revision=updateRevision;
  try{const update=await run();if(revision===updateRevision)setState({update});}catch(error){if(revision===updateRevision)setState({update:{...state.update,currentVersion:state.version||"dev",status:"error",error:String(error)}});}
}
export function updateBusy(value:AppState){return value.running||value.cancelling||value.preparingLocal||value.savingMode||value.publishing;}

bridge.onUpdate(update=>{updateRevision++;setState({update});});
bridge.onProgress(progress=>setState({progress,running:progress.state==="running",...(progress.state!=="running"?{cancelling:cancelRequestPending}:{})}));
bridge.onComplete(summary=>{
  setState({running:false,cancelling:cancelRequestPending,current:summary,currentResultRunId:summary.runId,history:[summary,...state.history.filter(item=>item.runId!==summary.runId)].slice(0,40),progress:state.progress?{...state.progress,state:summary.state,stages:state.progress.stages.map(stage=>stage.state==="running"?summary.state==="cancelled"?{...stage,state:"cancelled"}:{...stage,state:"completed",passed:summary.results.length,failed:Math.max(0,stage.input-summary.results.length),durationMs:stage.durationMs||28}:stage)}:undefined,currentRunId:undefined});
  void bridge.bootstrap().then(data=>{if(state.current?.runId===summary.runId)setState({sources:data.sources,network:data.network,resume:data.resume});});
});
bridge.onPublish(update=>applyPublication(update));

function applyPublication(update:PublicationUpdate){
  const key=`${update.runId}:${update.result.target}`;
  if(update.result.state==="queued"||update.result.state==="running")activePublications.add(key);else activePublications.delete(key);
  const merge=(summary:RunSummary):RunSummary=>summary.runId===update.runId?{...summary,publications:mergeTarget(summary.publications,update.result)}:summary;
  setState({publishing:activePublications.size>0,current:state.current?merge(state.current):state.current,history:state.history.map(merge)});
}

function mergeTarget(values:PublicationResult[],result:PublicationResult){
  return [...values.filter(value=>value.target!==result.target),result];
}

export function useAppStore<T>(selector:(value:AppState)=>T):T { return useSyncExternalStore(callback=>{listeners.add(callback);return()=>listeners.delete(callback);},()=>selector(state)); }
export const getAppState=()=>state;
