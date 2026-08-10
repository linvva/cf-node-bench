import { useState } from "react";
import { Button, Menu, Modal, Popover, ProgressBar, toast } from "@heroui/react";
import { Activity, AlertTriangle, Check, ChevronDown, ChevronRight, CircleStop, Gauge, GitMerge, Minus, Play, Plus, RefreshCw, ShieldCheck, Wifi, X } from "lucide-react";
import { actions, useAppStore } from "../../store";
import type { ResumeOption, RunStartRequest, StageProgress } from "../../types";
import { ResultsTable } from "../results/ResultsTable";
import { HistoryChart } from "./HistoryChart";

const stages=[{id:"source",label:"数据源",activity:"获取数据源"},{id:"filter",label:"解析 / 过滤",activity:"解析与过滤"},{id:"tcp",label:"TCP",activity:"TCP 探测"},{id:"https",label:"HTTPS",activity:"HTTPS 探测"},{id:"retained",label:"保留复检",activity:"复检上轮结果"},{id:"bandwidth",label:"带宽",activity:"带宽测试"},{id:"ranking",label:"排序",activity:"综合排序"}];
const failureLabels:Record<string,string>={invalid_ip:"无效 IP",invalid_port:"无效端口",invalid_tag:"无效标签",port_filtered:"端口排除",country_filtered:"国家排除",dns:"DNS",tcp:"TCP",tls:"TLS",timeout:"超时",http_status:"HTTP 状态",cancelled:"已取消",download:"下载"};

export function RunWorkspace(){
  const network=useAppStore(state=>state.network); const running=useAppStore(state=>state.running); const summary=useAppStore(state=>state.current); const history=useAppStore(state=>state.history); const sources=useAppStore(state=>state.sources); const resume=useAppStore(state=>state.resume);
  const [pendingResume,setPendingResume]=useState<ResumeOption>();
  const activeSources=sources.filter(source=>source.enabled).length;
  const run=async(request:RunStartRequest)=>{try{await actions.start(request);}catch(reason){toast.danger(`启动失败：${String(reason)}`);}};
  const chooseResume=(option:ResumeOption)=>{
    if(!option.available)return;
    if(option.warnings.length){setPendingResume(option);return;}
    void run({startStage:option.stage,baseRunId:resume.baseRunId,allowStaleCheckpoint:false});
  };
  return <div className="page" data-testid="run-page">
    <header className="page-header"><div><h1>测速工作台</h1><p>从当前设备发起多阶段网络测量，结果不会经过第三方测速服务。</p></div><div className="page-actions">{running?<Button variant="danger-soft" onPress={()=>void actions.cancel()}><CircleStop size={16}/>取消测速</Button>:<div className="run-start-actions"><Button variant="primary" onPress={()=>void run({startStage:"source",allowStaleCheckpoint:false})}><Play size={16}/>开始测速</Button><Popover><Popover.Trigger><Button isIconOnly variant="secondary" aria-label="选择测速起点"><ChevronDown size={16}/></Button></Popover.Trigger><Popover.Content placement="bottom end"><Popover.Dialog className="run-start-menu"><Popover.Heading>选择测速起点</Popover.Heading><Menu aria-label="阶段重测选项" onAction={key=>{const option=resume.options.find(item=>item.stage===String(key));if(option)chooseResume(option);}}>{resume.options.map(option=><Menu.Item id={option.stage} key={option.stage} isDisabled={!option.available} textValue={resumeLabel(option.stage)}><RefreshCw size={14}/><span><strong>{resumeLabel(option.stage)}</strong><small>{option.available?`${formatCount(option.input)} 个输入 · ${formatMeasuredAt(option.measuredAt)}`:"当前结果没有完整检查点"}</small></span>{option.warnings.length>0&&<AlertTriangle size={13}/>}</Menu.Item>)}</Menu>{resume.error&&<p className="checkpoint-menu-error">{resume.error}</p>}{resume.options.length===0&&!resume.error&&<p className="checkpoint-menu-empty">完成首轮测速后可从阶段重测</p>}</Popover.Dialog></Popover.Content></Popover></div>}</div></header>
    <section className="network-strip" aria-label="当前网络信息">
      <div className="network-item"><label>网络状态</label><strong className="network-status"><Wifi size={13} style={{display:"inline",marginRight:6}}/>{network.status==="online"?"已连接":"不可用"}</strong></div>
      <div className="network-item"><label>本机 IPv4</label><strong>{network.ipv4||"未检测到"}</strong></div>
      <div className="network-item"><label>网络接口</label><strong>{network.interface||"-"}</strong></div>
      <div className="network-item"><label>启用数据源</label><strong>{activeSources} 个</strong></div>
    </section>
    <RunProgressPanels/>
    <ResultsTable summary={summary}/>
    {history.length>0&&<HistoryChart history={history}/>}
    {pendingResume&&<Modal isOpen onOpenChange={open=>{if(!open)setPendingResume(undefined)}}><Modal.Trigger className="sr-only" aria-label="阶段检查点确认"><span/></Modal.Trigger><Modal.Backdrop><Modal.Container size="md"><Modal.Dialog><Modal.Header><Modal.Heading>确认使用阶段检查点</Modal.Heading><Modal.CloseTrigger aria-label="关闭"><X size={17}/></Modal.CloseTrigger></Modal.Header><Modal.Body><p>该操作会复用此前阶段的测量值，并从“{resumeLabel(pendingResume.stage)}”继续。</p><ul className="checkpoint-warning-list">{pendingResume.warnings.map(warning=><li key={warning}>{warning}</li>)}</ul><p className="checkpoint-warning-note">最终结果可能混合不同时刻的网络指标。</p></Modal.Body><Modal.Footer><Button variant="tertiary" onPress={()=>setPendingResume(undefined)}>取消</Button><Button variant="primary" onPress={()=>{const option=pendingResume;setPendingResume(undefined);void run({startStage:option.stage,baseRunId:resume.baseRunId,allowStaleCheckpoint:true});}}>仍然重测</Button></Modal.Footer></Modal.Dialog></Modal.Container></Modal.Backdrop></Modal>}
  </div>;
}

function RunProgressPanels(){
  const progress=useAppStore(state=>state.progress); const summary=useAppStore(state=>state.current);
  const failures=progress?.failures||summary?.failures||{}; const checkpointWarning=progress?.checkpointWarning||summary?.checkpointWarning;
  const stageValues=stages.map(stage=>({...stage,current:progress?.stages.find(item=>item.name===stage.id)}));
  const runningIndex=stageValues.findIndex(item=>item.current?.state==="running");
  const completedIndex=stageValues.reduce((last,item,index)=>item.current?.state==="completed"?index:last,-1);
  const activeIndex=runningIndex>=0?runningIndex:Math.max(0,completedIndex);
  const active=stageValues[activeIndex]; const metrics=stageActivity(active.current);
  const runState=progress?.state||summary?.state||"idle"; const isRunning=runState==="running";
  const title=isRunning?`${active.activity}进行中`:runState==="completed"?"最近测速已完成":runState==="cancelled"?"测速已取消":"等待开始测速";
  const failureEntries=Object.entries(failures).filter(([,count])=>count).sort((left,right)=>right[1]-left[1]);
  const failureTotal=failureEntries.reduce((total,[,count])=>total+count,0); const failureMax=failureEntries[0]?.[1]||1;
  const freshPath=[
    {label:"解析输入",value:stageValues[1].current?.input},
    {label:"过滤后",value:stageValues[1].current?.passed},
    {label:"TCP 通过",value:stageValues[2].current?.passed},
    {label:"HTTPS 通过",value:stageValues[3].current?.passed},
  ];
  const retainedPath=[
    {label:"复检输入",value:stageValues[4].current?.input},
    {label:"复检通过",value:stageValues[4].current?.passed},
  ];
  const mergedPath=[
    {label:"带宽通过",value:stageValues[5].current?.passed},
    {label:"最终结果",value:stageValues[6].current?.passed},
  ];
  return <div className="progress-stack">
    <section className="panel run-progress-panel" aria-label="当前测速进度">
      <div className="active-stage-summary">
        <div className="active-stage-title"><span><Gauge size={18}/></span><div><small>{isRunning?`阶段 ${activeIndex+1} / ${stages.length}`:"运行状态"}</small><h2>{title}</h2><p>{isRunning?metrics.detail:runState==="completed"?`${summary?.results.length??0} 个节点进入最终结果`:runState==="cancelled"?"保留上一次已完成的测速结果":"准备好后可开始新的网络测量"}</p></div></div>
        <div className="active-stage-metrics">
          <span><small>阶段进度</small><strong>{Math.round(metrics.percent)}%</strong></span>
          <span><small>处理速率</small><strong>{isRunning&&metrics.rate>0?`${formatRate(metrics.rate)} ${metrics.rateUnit}`:"-"}</strong></span>
          <span><small>已用时间</small><strong>{metrics.durationMs?formatDuration(metrics.durationMs):"-"}</strong></span>
          <span><small>预计剩余</small><strong>{isRunning?metrics.eta:"-"}</strong></span>
        </div>
        <ProgressBar className="active-stage-progress" aria-label={`${active.label}进度`} value={metrics.percent} size="sm"><ProgressBar.Track><ProgressBar.Fill/></ProgressBar.Track></ProgressBar>
      </div>
      <section className="stage-list" aria-label="测速阶段">
        {stageValues.map((stage,index)=>{const current=stage.current; const state=current?.state||"pending"; const attempts=current?.attemptsTotal?`探测 ${formatCount(current.attemptsCompleted??0)} / ${formatCount(current.attemptsTotal)}`:""; const processed=(current?.passed??0)+(current?.failed??0); const detail=state==="pending"?"等待执行":state==="running"?`${formatCount(processed)} / ${formatCount(current?.input??0)} 节点`:state==="reused"?`${formatCount(current?.passed??0)} 通过 · 已复用`:state==="skipped"?"本轮不适用":state==="cancelled"?"已取消 · 采样未保留":`${formatCount(current?.passed??0)} 通过 · ${formatCount(current?.failed??0)} 失败`; const timing=state==="running"?[attempts,formatDuration(current?.durationMs??0)].filter(Boolean).join(" · "):state==="reused"?formatMeasuredAt(current?.measuredAt):state==="skipped"?"":current?formatDuration(current.durationMs):""; return <div className="stage" data-state={state} key={stage.id}><span className="stage-node">{state==="completed"?<Check size={13}/>:state==="running"?<Activity size={13}/>:state==="reused"?<RefreshCw size={12}/>:state==="skipped"?<Minus size={12}/>:state==="cancelled"?<X size={12}/>:index+1}</span><div className="stage-copy"><strong>{stage.label}</strong><small>{detail}</small><div className="stage-time" title={attempts}>{timing}</div></div></div>})}
      </section>
      {checkpointWarning&&<div className="checkpoint-save-warning" role="status"><AlertTriangle size={14}/>{checkpointWarning}</div>}
    </section>
    <div className="run-insights">
      <section className="panel candidate-panel" aria-label="候选路径"><div className="insight-heading"><div><h2>候选路径</h2><span>新节点与上轮结果在带宽阶段汇合</span></div></div><div className="candidate-paths"><CandidateLane label="新节点" items={freshPath}/><Plus className="path-operator" size={15}/><CandidateLane label="上轮保留" items={retainedPath}/><GitMerge className="path-operator" size={15}/><CandidateLane label="汇合结果" items={mergedPath}/></div></section>
      <aside className="panel failure-panel"><div className="insight-heading"><div><h2>本轮失败</h2><span>{failureTotal?`${formatCount(failureTotal)} 条失败记录`:"当前没有失败记录"}</span></div></div>{failureEntries.length?<div className="failure-bars">{failureEntries.slice(0,4).map(([reason,count])=><div className="failure-row" key={reason}><span>{failureLabels[reason]||reason}</span><i><b style={{width:`${Math.max(4,count/failureMax*100)}%`}}/></i><strong>{formatCount(count)}</strong></div>)}</div>:<div className="empty-note"><ShieldCheck size={15}/> 尚无失败记录</div>}</aside>
    </div>
  </div>;
}

function CandidateLane({label,items}:{label:string;items:{label:string;value?:number}[]}){
  return <div className="candidate-lane"><small>{label}</small><div>{items.map((item,index)=><div className="funnel-step-wrap" key={item.label}><div className="funnel-step"><strong>{item.value===undefined?"-":formatCount(item.value)}</strong><span>{item.label}</span></div>{index<items.length-1&&<ChevronRight size={13}/>}</div>)}</div></div>;
}

function stageActivity(stage?:StageProgress){
  const attempts=stage?.attemptsTotal??0; const total=attempts||stage?.input||0;
  const completed=attempts?(stage?.attemptsCompleted??0):(stage?.passed??0)+(stage?.failed??0);
  const percent=total?Math.min(100,completed/total*100):stage?.state==="completed"||stage?.state==="reused"?100:0;
  const durationMs=stage?.durationMs??0; const rate=completed&&durationMs?completed/(durationMs/1000):0;
  const remaining=Math.max(0,total-completed); const eta=rate>0?formatRemaining(remaining/rate):"计算中";
  return {percent,durationMs,rate,eta,rateUnit:attempts?"次/秒":"项/秒",detail:total?`${formatCount(completed)} / ${formatCount(total)} ${attempts?"次探测":"项处理"}`:"正在准备阶段任务"};
}

function resumeLabel(stage:ResumeOption["stage"]){return {tcp:"从 TCP 开始",https:"从 HTTPS 开始",bandwidth:"仅重测带宽",ranking:"仅重新排序"}[stage];}

function formatMeasuredAt(value?:string){
  if(!value)return "时间未知";
  const date=new Date(value); if(Number.isNaN(date.getTime()))return "时间未知";
  return `测于 ${date.toLocaleTimeString("zh-CN",{hour:"2-digit",minute:"2-digit"})}`;
}

function formatDuration(durationMs:number){
  return durationMs<1000?`${durationMs} ms`:`${(durationMs/1000).toFixed(1)} s`;
}

function formatCount(value:number){
  return new Intl.NumberFormat("zh-CN",{notation:"compact",maximumFractionDigits:1}).format(value);
}

function formatRate(value:number){
  return new Intl.NumberFormat("zh-CN",{maximumFractionDigits:value>=100?0:1}).format(value);
}

function formatRemaining(seconds:number){
  if(seconds<1)return "即将完成";
  if(seconds<60)return `${Math.ceil(seconds)} 秒`;
  const minutes=Math.floor(seconds/60); const remaining=Math.ceil(seconds%60);
  return `${minutes}:${String(remaining).padStart(2,"0")}`;
}
