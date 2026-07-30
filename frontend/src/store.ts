import { useSyncExternalStore } from "react";
import { bridge } from "./lib/bridge";
import type { Bootstrap, HTTPSource, PublicationResult, PublicationUpdate, PublishCredentialTarget, PublishSaveRequest, PublishSettingsView, PublishTarget, RunProgress, RunSummary, Settings } from "./types";

interface AppState extends Bootstrap { ready:boolean; running:boolean; progress?:RunProgress; current?:RunSummary; error?:string }
const initial:AppState={ready:false,running:false,settings:{} as Settings,publishSettings:{} as PublishSettingsView,sources:[],history:[],network:{interface:"",ipv4:"",status:"unavailable"}};
let state=initial;
const listeners=new Set<()=>void>();
const setState=(patch:Partial<AppState>)=>{state={...state,...patch};listeners.forEach(listener=>listener());};

export const actions={
  async init(){ try { const data=await bridge.bootstrap(); setState({...data,ready:true,current:data.history[0]}); } catch(error){setState({ready:true,error:String(error)});} },
  async start(){
    const startedAt=new Date().toISOString();
    setState({running:true,error:undefined,progress:{runId:"",state:"running",startedAt,stages:[{name:"source",input:state.sources.filter(source=>source.enabled).length,passed:0,failed:0,durationMs:0,state:"running"}],failures:{}}});
    try {
      const id=await bridge.startRun();
      if(!state.running) return;
      setState({currentRunId:id,progress:state.progress?{...state.progress,runId:state.progress.runId||id}:undefined});
    } catch(error){setState({running:false,progress:undefined,error:String(error)});}
  },
  async cancel(){ await bridge.cancelRun(); },
  async saveSettings(settings:Settings){ await bridge.saveSettings(settings); setState({settings}); },
  async saveSources(sources:HTTPSource[]){ await bridge.saveSources(sources); setState({sources}); },
  async savePublishSettings(request:PublishSaveRequest){ const publishSettings=await bridge.savePublishSettings(request); setState({publishSettings}); return publishSettings; },
  async clearPublishCredential(target:PublishCredentialTarget){ const publishSettings=await bridge.clearPublishCredential(target); setState({publishSettings}); return publishSettings; },
  async testPublishTarget(target:PublishTarget,request:PublishSaveRequest){ await bridge.testPublishTarget(target,request); },
  async publishRun(runId:string,target:"all"|PublishTarget){ await bridge.publishRun(runId,target); },
};

bridge.onProgress(progress=>setState({progress,running:progress.state==="running"}));
bridge.onComplete(summary=>{
  setState({running:false,current:summary,history:[summary,...state.history.filter(item=>item.runId!==summary.runId)],progress:state.progress?{...state.progress,state:summary.state,stages:state.progress.stages.map(stage=>stage.state==="running"?{...stage,state:"completed",passed:summary.results.length,failed:Math.max(0,stage.input-summary.results.length),durationMs:stage.durationMs||28}:stage)}:undefined,currentRunId:undefined});
  void bridge.bootstrap().then(data=>setState({sources:data.sources,network:data.network}));
});
bridge.onPublish(update=>applyPublication(update));

function applyPublication(update:PublicationUpdate){
  const merge=(summary:RunSummary):RunSummary=>summary.runId===update.runId?{...summary,publications:mergeTarget(summary.publications,update.result)}:summary;
  setState({current:state.current?merge(state.current):state.current,history:state.history.map(merge)});
}

function mergeTarget(values:PublicationResult[],result:PublicationResult){
  return [...values.filter(value=>value.target!==result.target),result];
}

export function useAppStore<T>(selector:(value:AppState)=>T):T { return useSyncExternalStore(callback=>{listeners.add(callback);return()=>listeners.delete(callback);},()=>selector(state)); }
export const getAppState=()=>state;
