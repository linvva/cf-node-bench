import { Button } from "@heroui/react";
import { CircleStop, Play, ShieldCheck, Wifi } from "lucide-react";
import { actions, useAppStore } from "../../store";
import { ResultsTable } from "../results/ResultsTable";
import { HistoryChart } from "./HistoryChart";

const stages=[{id:"source",label:"数据源"},{id:"filter",label:"解析 / 过滤"},{id:"tcp",label:"TCP"},{id:"https",label:"HTTPS"},{id:"bandwidth",label:"带宽"},{id:"ranking",label:"排序"}];
const failureLabels:Record<string,string>={invalid_ip:"无效 IP",invalid_port:"无效端口",invalid_tag:"无效标签",port_filtered:"端口排除",country_filtered:"国家排除",dns:"DNS",tcp:"TCP",tls:"TLS",timeout:"超时",http_status:"HTTP 状态",cancelled:"已取消",download:"下载"};

export function RunWorkspace(){
  const network=useAppStore(state=>state.network); const running=useAppStore(state=>state.running); const progress=useAppStore(state=>state.progress); const summary=useAppStore(state=>state.current); const history=useAppStore(state=>state.history); const sources=useAppStore(state=>state.sources);
  const activeSources=sources.filter(source=>source.enabled).length;
  return <div className="page" data-testid="run-page">
    <header className="page-header"><div><h1>测速工作台</h1><p>从当前设备发起多阶段网络测量，结果不会经过第三方测速服务。</p></div><div className="page-actions">{running?<Button variant="danger-soft" onPress={()=>void actions.cancel()}><CircleStop size={16}/>取消测速</Button>:<Button variant="primary" onPress={()=>void actions.start()}><Play size={16}/>开始测速</Button>}</div></header>
    <section className="network-strip" aria-label="当前网络信息">
      <div className="network-item"><label>网络状态</label><strong className="network-status"><Wifi size={13} style={{display:"inline",marginRight:6}}/>{network.status==="online"?"已连接":"不可用"}</strong></div>
      <div className="network-item"><label>本机 IPv4</label><strong>{network.ipv4||"未检测到"}</strong></div>
      <div className="network-item"><label>网络接口</label><strong>{network.interface||"-"}</strong></div>
      <div className="network-item"><label>启用数据源</label><strong>{activeSources} 个</strong></div>
    </section>
    <div className="progress-layout">
      <section className="panel stage-list" aria-label="测速阶段">
        {stages.map(stage=>{const current=progress?.stages.find(item=>item.name===stage.id); const state=current?.state||"pending"; return <div className="stage" data-state={state} key={stage.id}><div className="stage-head"><span>{stage.label}</span><i className="stage-state"/></div><div className="stage-metrics"><span>输入<strong>{current?.input??0}</strong></span><span>通过<strong>{current?.passed??0}</strong></span><span>失败<strong>{current?.failed??0}</strong></span></div><div className="stage-time">{state==="running"?"正在处理…":current?`${current.durationMs} ms`:"等待执行"}</div></div>})}
      </section>
      <aside className="panel failure-panel"><h2>累计失败项</h2><div className="failure-list">{Object.entries(progress?.failures||summary?.failures||{}).filter(([,count])=>count).map(([reason,count])=><span className="failure-chip" key={reason}>{failureLabels[reason]||reason}<b>{count}</b></span>)}</div>{!Object.values(progress?.failures||summary?.failures||{}).some(Boolean)&&<div className="empty-note"><ShieldCheck size={15}/> 尚无失败记录</div>}</aside>
    </div>
    <ResultsTable summary={summary}/>
    {history.length>0&&<HistoryChart history={history}/>}
  </div>;
}
