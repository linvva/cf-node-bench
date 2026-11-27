import { useEffect, useState } from "react";
import { Button, Menu, Modal, Popover, ProgressBar, Switch, toast } from "@heroui/react";
import { Activity, AlertTriangle, Check, ChevronDown, FolderOpen, ChevronRight, CircleStop, Gauge, GitMerge, Minus, Play, Plus, RefreshCw, ShieldCheck, Wifi, X } from "lucide-react";
import { actions, useAppStore } from "../../store";
import type { ResumeOption, RunStartRequest, RunSummary, StageProgress } from "../../types";
import { ResultsTable } from "../results/ResultsTable";
import { downloadCheckText, failureCounts } from "../../lib/failureLabels";
import { ProtocolTabs } from "../ProtocolTabs";

const tcpStages=[{id:"source",label:"数据源",activity:"获取数据源"},{id:"filter",label:"解析 / 过滤",activity:"解析与过滤"},{id:"tcp",label:"TCP",activity:"TCP 探测"},{id:"https",label:"HTTPS",activity:"HTTPS 探测"},{id:"retained",label:"保留复检",activity:"复检上轮结果"},{id:"bandwidth",label:"带宽",activity:"带宽测试"},{id:"ranking",label:"排序",activity:"综合排序"}];

export function RunWorkspace(){
  const network=useAppStore(state=>state.network); const running=useAppStore(state=>state.running); const summary=useAppStore(state=>state.current); const history=useAppStore(state=>state.history); const sources=useAppStore(state=>state.sources); const resume=useAppStore(state=>state.resume);
  const settings=useAppStore(state=>state.settings);
  const progress=useAppStore(state=>state.progress);
  const cancelling=useAppStore(state=>state.cancelling);
  const runId=progress?.runId;
  const [pendingCancel,setPendingCancel]=useState<string>();
  const [pendingResume,setPendingResume]=useState<{option:ResumeOption; continuation:boolean}>();
  const [historyRunId,setHistoryRunId]=useState<string>();
  useEffect(()=>{setHistoryRunId(undefined);setPendingResume(undefined);},[summary?.runId]);
  useEffect(()=>{if(!running||pendingCancel!==runId)setPendingCancel(undefined);},[running,runId,pendingCancel]);
  const displayedSummary=history.find(item=>item.runId===historyRunId)??summary;
  const activeSources=sources.filter(source=>source.enabled).length;
  const savingH3=useAppStore(state=>state.savingMode);
  const preparingLocal=useAppStore(state=>state.preparingLocal);
  const quick=useAppStore(state=>state.quickSelection);
  const [savingRetention,setSavingRetention]=useState(false);
  const controlsBusy=running||cancelling||preparingLocal||savingH3||savingRetention;
  const continuation=resume.continueBandwidth;
  const inBandwidth=progress?.stages.some(stage=>stage.name==="bandwidth"&&stage.state==="running");
  const toggleRetention=async(value:boolean)=>{
    setSavingRetention(true);
    try{await actions.saveSettings({...settings,retainPreviousResults:value});}
    catch(reason){toast.danger(`保存失败：${String(reason)}`);}
    finally{setSavingRetention(false);}
  };
  const setRequireH3=async(value:boolean)=>{try{await actions.setRequireH3(value);}catch(reason){toast.danger(String(reason));}};
  const runLocal=async()=>{try{await actions.startLocal();}catch(reason){toast.danger(`启动失败：${String(reason)}`);}};
  const run=async(request:RunStartRequest)=>{try{await actions.start(request);}catch(reason){toast.danger(`启动失败：${String(reason)}`);}};
  const cancel=async(keepResults:boolean,id:string,saveBandwidth=false)=>{
    try{await actions.cancel(keepResults,id,saveBandwidth);setPendingCancel(undefined);}catch(reason){toast.danger(`取消失败：${String(reason)}`);}
  };
  const chooseCancel=()=>{
    if(!runId)return;
    if((progress?.availableResults??0)>0)setPendingCancel(runId);
    else void cancel(false,runId);
  };
  const chooseResume=(option:ResumeOption,continuation=false)=>{
    if(!option.available)return;
    if(option.warnings.length){setPendingResume({option,continuation});return;}
    void run({startStage:option.stage,baseRunId:resume.baseRunId,allowStaleCheckpoint:false,...(continuation?{continueBandwidth:true}:{})});
  };
  return <div className="page" data-testid="run-page">
    <header className="page-header"><div><h1>测速工作台</h1></div></header>
    <section className="panel run-controls" aria-label="开始测速">
      <div className="run-options">
        <ProtocolTabs label="测速模式" value={settings.requireH3?"h3":"tcp_https"} disabled={controlsBusy} onChange={mode=>void setRequireH3(mode==="h3")}/>
        <label className="run-strategy">筛选策略
          <select value={quick?"quick":"complete"} disabled={controlsBusy} onChange={event=>actions.setQuickSelection(event.target.value==="quick")}>
            <option value="complete">完整筛选</option><option value="quick">快速筛选</option>
          </select>
        </label>
        <Switch className="retention-switch" aria-label="上轮节点复测" isSelected={settings.retainPreviousResults} isDisabled={controlsBusy} onChange={value=>void toggleRetention(value)}><Switch.Content><Switch.Control><Switch.Thumb/></Switch.Control><span>上轮节点复测</span></Switch.Content></Switch>
      </div>
      <p className="run-control-note">{quick?"按连接表现分批做 HTTP 与带宽测试，达到新节点目标后结束。":"验证所有连接合格节点的 HTTP 表现，再按表现安排带宽测试。"}</p>
      <div className="page-actions run-control-actions">
        {running?<>
          <Button variant="secondary" isDisabled><FolderOpen size={16}/>本地文件测速</Button>
          {inBandwidth&&runId&&<Button variant="secondary" isDisabled={cancelling} onPress={()=>void cancel(false,runId,true)}>保存进度并停止</Button>}
          <Button variant="danger-soft" isDisabled={preparingLocal||cancelling||!runId} onPress={chooseCancel}><CircleStop size={16}/>取消测速</Button>
        </>:<>
          <div className="run-start-actions">
            <Button variant="primary" isDisabled={controlsBusy} onPress={()=>void run({startStage:"source",allowStaleCheckpoint:false})}><Play size={16}/>开始测速</Button>
            <Popover><Popover.Trigger><Button isIconOnly variant="secondary" isDisabled={controlsBusy} aria-label="选择测速起点"><ChevronDown size={16}/></Button></Popover.Trigger>
              <Popover.Content placement="bottom start"><Popover.Dialog className="run-start-menu"><Popover.Heading>阶段重测</Popover.Heading>
                <Menu aria-label="阶段重测选项" onAction={key=>{const option=resume.options.find(item=>item.stage===String(key));if(option)chooseResume(option);}}>
                  {resume.options.map(option=><Menu.Item id={option.stage} key={option.stage} isDisabled={!option.available} textValue={resumeLabel(option.stage)}>
                    <RefreshCw size={14}/><span><strong>{resumeLabel(option.stage)}</strong><small>{option.available?`${formatCount(option.input)} 个输入 · ${formatMeasuredAt(option.measuredAt)}`:option.warnings[0]||"当前结果没有完整检查点"}</small></span>{option.warnings.length>0&&<AlertTriangle size={13}/>}
                  </Menu.Item>)}
                </Menu>
                {resume.error&&<p className="checkpoint-menu-error">{resume.error}</p>}
                {resume.options.length===0&&!resume.error&&<p className="checkpoint-menu-empty">完成首轮测速后可从阶段重测</p>}
              </Popover.Dialog></Popover.Content>
            </Popover>
          </div>
          <Button variant="secondary" isDisabled={controlsBusy} onPress={()=>void runLocal()}><FolderOpen size={16}/>本地文件测速</Button>
          {continuation&&<Button variant="secondary" isDisabled={controlsBusy||!continuation.available} onPress={()=>chooseResume(continuation,true)}><RefreshCw size={15}/>继续带宽测速</Button>}
        </>}
      </div>
      {!running&&<p className="run-control-note">{continuation
        ? continuation.available?`已保存 ${formatCount(continuation.input)} 个地址的带宽测量`:continuation.warnings.join("。")
        : `使用 ${activeSources} 个启用的数据源，或选择、拖入 CSV / TXT / JSON 文件。`}</p>}
    </section>
    <section className="network-strip" aria-label="当前网络信息">
      <div className="network-item"><label>网络状态</label><strong className="network-status"><Wifi size={13} style={{display:"inline",marginRight:6}}/>{network.status==="online"?"已连接":"不可用"}</strong></div>
      <div className="network-item"><label>本机 IPv4</label><strong>{network.ipv4||"未检测到"}</strong></div>
      <div className="network-item"><label>网络接口</label><strong>{network.interface||"-"}</strong></div>
      <div className="network-item"><label>启用数据源</label><strong>{activeSources} 个</strong></div>
    </section>
    {running&&<RunProgressPanels active/>}
    <ResultsTable summary={displayedSummary} historyControl={history.length>0&&<select className="run-history" aria-label="查看历史运行" value={displayedSummary?.runId??""} onChange={event=>setHistoryRunId(event.target.value)}>
      {history.map(item=><option key={item.runId} value={item.runId}>{historyLabel(item)}</option>)}
    </select>}/>
    {!running&&displayedSummary&&<RunProgressPanels summary={displayedSummary} key={displayedSummary.runId}/>}
    {pendingCancel&&<Modal isOpen onOpenChange={open=>{if(!open&&!cancelling)setPendingCancel(undefined)}}><Modal.Trigger className="sr-only" aria-label="取消测速确认" tabIndex={-1}><span/></Modal.Trigger><Modal.Backdrop isDismissable={!cancelling} isKeyboardDismissDisabled={cancelling}><Modal.Container size="md"><Modal.Dialog><Modal.Header><Modal.Heading>如何处理已通过节点？</Modal.Heading><Modal.CloseTrigger aria-label="继续测速并关闭" isDisabled={cancelling}><X size={17}/></Modal.CloseTrigger></Modal.Header><Modal.Body><p>当前有 {progress?.availableResults??0} 个节点已完整完成连接、HTTP 和带宽测速并通过门槛。</p><p>选择期间测速仍会继续，以结束时实际完成的结果为准。保留结果仍按最终结果数选优，已完整测完的固化节点额外加入；正在采样的节点不保留。保留后可查看、复制、导出和手动发布，不会自动发布。</p></Modal.Body><Modal.Footer style={{display:"grid",gridTemplateColumns:"1fr"}}><Button variant="tertiary" isDisabled={cancelling} onPress={()=>setPendingCancel(undefined)}>继续测速</Button><Button variant="danger-soft" isDisabled={cancelling} onPress={()=>void cancel(false,pendingCancel)}>丢弃本轮结果并取消</Button><Button variant="primary" isDisabled={cancelling} onPress={()=>void cancel(true,pendingCancel)}>保留已通过节点并结束</Button></Modal.Footer></Modal.Dialog></Modal.Container></Modal.Backdrop></Modal>}
    {pendingResume&&<Modal isOpen onOpenChange={open=>{if(!open)setPendingResume(undefined)}}><Modal.Trigger className="sr-only" aria-label="阶段检查点确认"><span/></Modal.Trigger><Modal.Backdrop><Modal.Container size="md"><Modal.Dialog><Modal.Header><Modal.Heading>确认使用阶段检查点</Modal.Heading><Modal.CloseTrigger aria-label="关闭"><X size={17}/></Modal.CloseTrigger></Modal.Header><Modal.Body><p>{pendingResume.continuation?"将复用已保存的带宽测量，继续测试余下节点。":"将复用此前阶段的测量值。所选操作为「"+resumeLabel(pendingResume.option.stage)+"」。"}</p><ul className="checkpoint-warning-list">{pendingResume.option.warnings.map(warning=><li key={warning}>{warning}</li>)}</ul><p className="checkpoint-warning-note">最终结果可能混合不同时刻的网络指标。</p></Modal.Body><Modal.Footer><Button variant="tertiary" onPress={()=>setPendingResume(undefined)}>取消</Button><Button variant="primary" onPress={()=>{const {option,continuation}=pendingResume;setPendingResume(undefined);void run({startStage:option.stage,baseRunId:resume.baseRunId,allowStaleCheckpoint:true,...(continuation?{continueBandwidth:true}:{})});}}>{pendingResume.continuation?"仍然继续":"仍然重测"}</Button></Modal.Footer></Modal.Dialog></Modal.Container></Modal.Backdrop></Modal>}
  </div>;
}

function historyLabel(summary:RunSummary){
  const date=new Date(summary.finishedAt);
  const timestamp=date.toLocaleString("zh-CN",{month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",hour12:false});
  const elapsed=Math.max(0,date.getTime()-new Date(summary.startedAt).getTime());
  const qualified=summary.results.filter(result=>result.status==="qualified").length;
  const state=summary.partialResults?"已取消，已保留部分结果":summary.bandwidthSaved?"已保存进度":summary.state==="completed"?"已完成":summary.state==="cancelled"?"已取消":"运行失败";
  return `${timestamp} · ${summary.mode==="h3"?"H3":"TCP/HTTPS"} · ${summary.localFiles?"本地文件":"在线源"} · ${summary.quick?"快速":"完整"} · ${formatDuration(elapsed)} · ${qualified} 个达标 · ${state}`;
}

function RunProgressPanels({active=false,summary}:{active?:boolean;summary?:RunSummary}){
  const latestProgress=useAppStore(state=>state.progress);
  const progress=active||latestProgress?.runId===summary?.runId?latestProgress:undefined;
  const run=active?progress:summary;
  const isH3=run?.mode==="h3";
  const protocolStages=isH3?[...tcpStages.slice(0,2),{id:"dns",label:"DNS",activity:"解析 IPv4"},{id:"quic",label:"QUIC",activity:"QUIC 握手"},{id:"http3",label:"HTTP/3",activity:"HTTP/3 验证"},...tcpStages.slice(5)]:tcpStages;
  const stages=protocolStages.map(stage=>stage.id==="source"&&run?.localFiles?{...stage,label:"本地文件",activity:"读取本地文件"}:stage);
  const measuredStages=run?.stages??progress?.stages??[];
  const stageValue=(name:string)=>measuredStages.find(stage=>stage.name===name);
  const checkpointWarning=run?.checkpointWarning;
  const check=run?.downloadCheck;
  const stageValues=stages.map(stage=>({...stage,current:stageValue(stage.id)}));
  const currentStage=stageValues.find(item=>item.current?.state==="running")
    ??[...stageValues].reverse().find(item=>item.current?.state==="completed"||item.current?.state==="reused")
    ??stageValues[0];
  const metrics=stageActivity(currentStage.current);
  const scopedFailures=stageValues.flatMap(stage=>{
    const reasons=failureCounts(stage.current?.failures);
    return reasons.length?[{label:stage.label,reasons}]:[];
  });
  const aggregateFailures=failureCounts(run?.failures);
  const httpStage=stageValue(isH3?"http3":"https");
  const httpInput=(httpStage?.input??0)+(isH3?0:stageValue("retained")?.input??0);
  const downloadStage=stageValue("bandwidth"),queue=downloadStage?.queue;
  const freshPath=[
    {label:"解析输入",value:stageValue("filter")?.input},
    {label:"过滤后",value:stageValues[1].current?.passed},
    {label:isH3?"QUIC 通过":"TCP 通过",value:stageValue(isH3?"quic":"tcp")?.passed},
    {label:httpStage?.selected===undefined?(isH3?"HTTP/3 通过":"HTTPS 通过"):(isH3?"新节点入选":"HTTPS 入选"),value:httpStage?.selected??httpStage?.passed},
  ];
  const retainedPath=[
    {label:"复检输入",value:stageValue("retained")?.input},
    {label:"复检通过",value:stageValue("retained")?.passed},
  ];
  const mergedPath=[
    {label:"带宽通过",value:stageValue("bandwidth")?.passed},
    {label:"最终结果",value:stageValue("ranking")?.passed},
  ];
  return <div className="progress-stack">
    {check&&<div className={`panel download-check-note${check.sampled&&!check.passed?" warning":""}`} role={check.sampled&&!check.passed?"alert":"status"}>
      <strong>下载地址预检</strong><span>{downloadCheckText(check)}{check.sampled&&!check.passed&&active?"。可取消测速并检查下载地址。":""}</span>
    </div>}
    {active&&<section className="panel run-progress-panel" aria-label="当前测速进度">
      <div className="active-stage-summary">
        <div className="active-stage-title"><span><Gauge size={18}/></span><div><small>{run?.quick?"快速筛选":"完整筛选"}</small><h2>{currentStage.activity}进行中</h2><p>{metrics.detail}</p>{metrics.status&&<p>{metrics.status}</p>}{!!currentStage.current?.attemptsTotal&&!currentStage.current.queue&&<p>{formatCount(currentStage.current.passed+currentStage.current.failed)} / {formatCount(currentStage.current.input)} 节点</p>}</div></div>
        <div className="active-stage-metrics">
          <span><small>{currentStage.current?.queue?"目标进度":"阶段进度"}</small><strong>{Math.round(metrics.percent)}%</strong></span>
          <span><small>处理速率</small><strong>{metrics.rate>0?`${formatRate(metrics.rate)} ${metrics.rateUnit}`:"-"}</strong></span>
          <span><small>阶段用时</small><strong>{metrics.durationMs?formatDuration(metrics.durationMs):"-"}</strong></span>
          {metrics.eta&&<span><small>本阶段预计剩余</small><strong>{metrics.eta}</strong></span>}
        </div>
        <ProgressBar className="active-stage-progress" aria-label={`${currentStage.label}进度`} value={metrics.percent} size="sm"><ProgressBar.Track><ProgressBar.Fill/></ProgressBar.Track></ProgressBar>
      </div>
    </section>}
    {checkpointWarning&&<div className="checkpoint-save-warning" role="status"><AlertTriangle size={14}/>{checkpointWarning}</div>}
    <details className="panel run-details">
      <summary>测速详情{!active&&run?.quick?" · 快速筛选":""}{httpStage?.input!==undefined&&!active?` · ${isH3?"HTTP/3":"HTTPS"} 已测 ${formatCount(httpInput)} 个节点`:""}</summary>
      <section className="stage-list" aria-label="测速阶段">
        {stageValues.map((stage,index)=>{const current=stage.current; const state=current?.state||"pending"; const attempts=current?.queue?`已完成 ${formatCount(current.passed+current.failed)} 节点`:current?.attemptsTotal?`探测 ${formatCount(current.attemptsCompleted??0)} / ${formatCount(current.attemptsTotal)}`:""; const skipped=current?.attemptsSkipped?`提前跳过 ${formatCount(current.attemptsSkipped)} 次`:""; const processed=(current?.passed??0)+(current?.failed??0); const detail=stage.id==="ranking"&&state==="completed"?`已选出 ${formatCount(summary?.results.length??current?.passed??0)} 个节点`:state==="pending"?"等待执行":state==="running"?(current?.queue?`新节点通过 ${formatCount(current.queue.passed)} / ${formatCount(current.queue.target)}`:`${formatCount(processed)} / ${formatCount(current?.input??0)} 节点`):state==="reused"?`${formatCount(current?.passed??0)} 通过 · 已复用`:state==="skipped"?"本轮不适用":state==="cancelled"?"已取消 · 阶段未完成":`${formatCount(current?.passed??0)} 通过 · ${formatCount(current?.failed??0)} 失败${current?.selected===undefined?"":` · 入选 ${formatCount(current.selected)}`}`; const timing=state==="running"?[attempts,formatDuration(current?.durationMs??0)].filter(Boolean).join(" · "):state==="reused"?formatMeasuredAt(current?.measuredAt):state==="skipped"?"":current?formatDuration(current.durationMs):""; return <div className="stage" data-state={state} key={stage.id}><span className="stage-node">{state==="completed"?<Check size={13}/>:state==="running"?<Activity size={13}/>:state==="reused"?<RefreshCw size={12}/>:state==="skipped"?<Minus size={12}/>:state==="cancelled"?<X size={12}/>:index+1}</span><div className="stage-copy"><strong>{stage.label}</strong><small title={detail}>{detail}</small>{skipped&&<small>{skipped}</small>}<div className="stage-time" title={attempts}>{timing}</div></div></div>})}
      </section>
      {queue&&<div className="download-queue-stats" aria-label="带宽新节点队列"><span>新节点候选池：{formatCount(queue.pool)}</span><span>累计安排：{formatCount(queue.planned)}</span><span>待测：{formatCount(queue.pending)}</span><span>在测：{formatCount(queue.active)}</span><span>新节点通过：{formatCount(queue.passed)} / {formatCount(queue.target)}</span><span>本阶段已完成：{formatCount((downloadStage?.passed??0)+(downloadStage?.failed??0))}（含保留复测）</span>{(downloadStage?.state==="completed"||downloadStage?.state==="reused")&&queue.passed<queue.target&&<span>本轮候选已测完，未达到目标</span>}</div>}
      {isH3&&<div className="h3-progress-stats" aria-label="H3 检测数量"><span>逻辑候选：{formatCount((progress?(progress.logicalCandidates??0):(summary?.logicalCandidates??0)))}</span><span>实际 IPv4：{formatCount((progress?(progress.physicalCandidates??0):(summary?.physicalCandidates??0)))}</span><span>累计下载：{(((progress?(progress.downloadedBytes??0):(summary?.downloadedBytes??0)))/1048576).toFixed(1)} MiB</span></div>}

    <div className="run-insights">
      <section className="panel candidate-panel" aria-label="候选路径"><div className="insight-heading"><div><h2>候选路径</h2><span>{isH3?"保留节点与新节点共同完成 QUIC 和 HTTP/3 验证":"新节点与上轮结果在带宽阶段汇合"}</span></div></div><div className="candidate-paths"><CandidateLane label={isH3?"全部候选":"新节点"} items={freshPath}/>{!isH3&&<><Plus className="path-operator" size={15}/><CandidateLane label="上轮保留" items={retainedPath}/></>}<GitMerge className="path-operator" size={15}/><CandidateLane label="汇合结果" items={mergedPath}/></div></section>
      <aside className="panel failure-panel"><div className="insight-heading"><h2>失败原因</h2></div>
        {scopedFailures.length?<ul className="stage-failures">{scopedFailures.map(stage=><li key={stage.label}><strong>{stage.label}</strong><span>{stage.reasons.join("、")}</span></li>)}</ul>
          :aggregateFailures.length?<p className="empty-note">{aggregateFailures.join("、")}</p>
          :<div className="empty-note"><ShieldCheck size={15}/> 尚无失败记录</div>}
      </aside>
    </div>
    </details>
  </div>;
}

function CandidateLane({label,items}:{label:string;items:{label:string;value?:number}[]}){
  return <div className="candidate-lane"><small>{label}</small><div>{items.map((item,index)=><div className="funnel-step-wrap" key={item.label}><div className="funnel-step"><strong>{item.value===undefined?"-":formatCount(item.value)}</strong><span>{item.label}</span></div>{index<items.length-1&&<ChevronRight size={13}/>}</div>)}</div></div>;
}

function stageActivity(stage?:StageProgress){
  if(stage?.queue){
    const queue=stage.queue,completed=stage.passed+stage.failed,durationMs=stage.durationMs;
    const percent=queue.target?Math.min(100,queue.passed/queue.target*100):100;
    const detail=`新节点通过 ${formatCount(queue.passed)} / ${formatCount(queue.target)} · 在测 ${formatCount(queue.active)} · 待测 ${formatCount(queue.pending)}`;
    const status=queue.passed>=queue.target&&queue.active>0?"等待在测完成":"";
    return {percent,durationMs,rate:durationMs?completed/(durationMs/1000):0,eta:undefined,rateUnit:"节点/秒",detail,status};
  }
  const attempts=stage?.attemptsTotal??0; const total=attempts||stage?.input||0;
  const completed=attempts?(stage?.attemptsCompleted??0):(stage?.passed??0)+(stage?.failed??0);
  const skipped=attempts?(stage?.attemptsSkipped??0):0; const processed=completed+skipped;
  const percent=total?Math.min(100,processed/total*100):stage?.state==="completed"||stage?.state==="reused"?100:0;
  const durationMs=stage?.durationMs??0; const rate=completed&&durationMs?completed/(durationMs/1000):0;
  const processedRate=processed&&durationMs?processed/(durationMs/1000):0;
  const remaining=Math.max(0,total-processed); const eta=processedRate>0&&remaining>0?formatRemaining(remaining/processedRate):undefined;
  return {percent,durationMs,rate,eta,rateUnit:attempts?"次/秒":"项/秒",status:undefined,detail:total?`${formatCount(completed)} / ${formatCount(total)} ${attempts?"次探测":"项处理"}${skipped?` · 提前跳过 ${formatCount(skipped)} 次`:""}`:"正在准备阶段任务"};
}

function resumeLabel(stage:ResumeOption["stage"]){return {quic:"从 QUIC 开始",http3:"从 HTTP/3 开始",tcp:"从 TCP 开始",https:"从 HTTPS 开始",bandwidth:"仅重测带宽",ranking:"仅重新排序"}[stage];}

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
