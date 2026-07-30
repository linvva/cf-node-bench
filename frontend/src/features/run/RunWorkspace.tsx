import { Button, ProgressBar } from "@heroui/react";
import { Activity, Check, ChevronRight, CircleStop, Gauge, Play, ShieldCheck, Wifi } from "lucide-react";
import { actions, useAppStore } from "../../store";
import type { StageProgress } from "../../types";
import { ResultsTable } from "../results/ResultsTable";
import { HistoryChart } from "./HistoryChart";

const stages=[{id:"source",label:"数据源",activity:"获取数据源"},{id:"filter",label:"解析 / 过滤",activity:"解析与过滤"},{id:"tcp",label:"TCP",activity:"TCP 探测"},{id:"https",label:"HTTPS",activity:"HTTPS 探测"},{id:"bandwidth",label:"带宽",activity:"带宽测试"},{id:"ranking",label:"排序",activity:"综合排序"}];
const failureLabels:Record<string,string>={invalid_ip:"无效 IP",invalid_port:"无效端口",invalid_tag:"无效标签",port_filtered:"端口排除",country_filtered:"国家排除",dns:"DNS",tcp:"TCP",tls:"TLS",timeout:"超时",http_status:"HTTP 状态",cancelled:"已取消",download:"下载"};

export function RunWorkspace(){
  const network=useAppStore(state=>state.network); const running=useAppStore(state=>state.running); const summary=useAppStore(state=>state.current); const history=useAppStore(state=>state.history); const sources=useAppStore(state=>state.sources);
  const activeSources=sources.filter(source=>source.enabled).length;
  return <div className="page" data-testid="run-page">
    <header className="page-header"><div><h1>测速工作台</h1><p>从当前设备发起多阶段网络测量，结果不会经过第三方测速服务。</p></div><div className="page-actions">{running?<Button variant="danger-soft" onPress={()=>void actions.cancel()}><CircleStop size={16}/>取消测速</Button>:<Button variant="primary" onPress={()=>void actions.start()}><Play size={16}/>开始测速</Button>}</div></header>
    <section className="network-strip" aria-label="当前网络信息">
      <div className="network-item"><label>网络状态</label><strong className="network-status"><Wifi size={13} style={{display:"inline",marginRight:6}}/>{network.status==="online"?"已连接":"不可用"}</strong></div>
      <div className="network-item"><label>本机 IPv4</label><strong>{network.ipv4||"未检测到"}</strong></div>
      <div className="network-item"><label>网络接口</label><strong>{network.interface||"-"}</strong></div>
      <div className="network-item"><label>启用数据源</label><strong>{activeSources} 个</strong></div>
    </section>
    <RunProgressPanels/>
    <ResultsTable summary={summary}/>
    {history.length>0&&<HistoryChart history={history}/>}
  </div>;
}

function RunProgressPanels(){
  const progress=useAppStore(state=>state.progress); const summary=useAppStore(state=>state.current);
  const failures=progress?.failures||summary?.failures||{};
  const stageValues=stages.map(stage=>({...stage,current:progress?.stages.find(item=>item.name===stage.id)}));
  const runningIndex=stageValues.findIndex(item=>item.current?.state==="running");
  const completedIndex=stageValues.reduce((last,item,index)=>item.current?.state==="completed"?index:last,-1);
  const activeIndex=runningIndex>=0?runningIndex:Math.max(0,completedIndex);
  const active=stageValues[activeIndex]; const metrics=stageActivity(active.current);
  const runState=progress?.state||summary?.state||"idle"; const isRunning=runState==="running";
  const title=isRunning?`${active.activity}进行中`:runState==="completed"?"最近测速已完成":runState==="cancelled"?"测速已取消":"等待开始测速";
  const failureEntries=Object.entries(failures).filter(([,count])=>count).sort((left,right)=>right[1]-left[1]);
  const failureTotal=failureEntries.reduce((total,[,count])=>total+count,0); const failureMax=failureEntries[0]?.[1]||1;
  const funnel=[
    {label:"解析输入",value:stageValues[1].current?.input},
    {label:"过滤后",value:stageValues[1].current?.passed},
    {label:"TCP 通过",value:stageValues[2].current?.passed},
    {label:"HTTPS 通过",value:stageValues[3].current?.passed},
    {label:"带宽通过",value:stageValues[4].current?.passed},
    {label:"最终结果",value:stageValues[5].current?.passed},
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
        {stageValues.map((stage,index)=>{const current=stage.current; const state=current?.state||"pending"; const attempts=current?.attemptsTotal?`探测 ${formatCount(current.attemptsCompleted??0)} / ${formatCount(current.attemptsTotal)}`:""; const processed=(current?.passed??0)+(current?.failed??0); return <div className="stage" data-state={state} key={stage.id}><span className="stage-node">{state==="completed"?<Check size={13}/>:state==="running"?<Activity size={13}/>:index+1}</span><div className="stage-copy"><strong>{stage.label}</strong><small>{state==="pending"?"等待执行":state==="running"?`${formatCount(processed)} / ${formatCount(current?.input??0)} 节点`:`${formatCount(current?.passed??0)} 通过 · ${formatCount(current?.failed??0)} 失败`}</small><div className="stage-time" title={attempts}>{state==="running"?[attempts,formatDuration(current?.durationMs??0)].filter(Boolean).join(" · "):current?formatDuration(current.durationMs):""}</div></div></div>})}
      </section>
    </section>
    <div className="run-insights">
      <section className="panel candidate-panel" aria-label="候选漏斗"><div className="insight-heading"><div><h2>候选漏斗</h2><span>节点通过每个阶段后的剩余数量</span></div></div><div className="funnel-list">{funnel.map((item,index)=><div className="funnel-step-wrap" key={item.label}><div className="funnel-step"><strong>{item.value===undefined?"-":formatCount(item.value)}</strong><span>{item.label}</span></div>{index<funnel.length-1&&<ChevronRight size={15}/>}</div>)}</div></section>
      <aside className="panel failure-panel"><div className="insight-heading"><div><h2>累计失败</h2><span>{failureTotal?`${formatCount(failureTotal)} 条失败记录`:"当前没有失败记录"}</span></div></div>{failureEntries.length?<div className="failure-bars">{failureEntries.slice(0,4).map(([reason,count])=><div className="failure-row" key={reason}><span>{failureLabels[reason]||reason}</span><i><b style={{width:`${Math.max(4,count/failureMax*100)}%`}}/></i><strong>{formatCount(count)}</strong></div>)}</div>:<div className="empty-note"><ShieldCheck size={15}/> 尚无失败记录</div>}</aside>
    </div>
  </div>;
}

function stageActivity(stage?:StageProgress){
  const attempts=stage?.attemptsTotal??0; const total=attempts||stage?.input||0;
  const completed=attempts?(stage?.attemptsCompleted??0):(stage?.passed??0)+(stage?.failed??0);
  const percent=total?Math.min(100,completed/total*100):stage?.state==="completed"?100:0;
  const durationMs=stage?.durationMs??0; const rate=completed&&durationMs?completed/(durationMs/1000):0;
  const remaining=Math.max(0,total-completed); const eta=rate>0?formatRemaining(remaining/rate):"计算中";
  return {percent,durationMs,rate,eta,rateUnit:attempts?"次/秒":"项/秒",detail:total?`${formatCount(completed)} / ${formatCount(total)} ${attempts?"次探测":"项处理"}`:"正在准备阶段任务"};
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
