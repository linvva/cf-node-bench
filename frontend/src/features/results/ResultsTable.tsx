import { memo, useEffect, useMemo, useState } from "react";
import { Button, Checkbox, Drawer, Input, Popover, Switch, Table, toast } from "@heroui/react";
import { Clipboard, Download, RotateCw, Search, Send, SlidersHorizontal, X } from "lucide-react";
import { actions, useAppStore } from "../../store";
import { connectionWeight, httpWeight } from "../../lib/scoring";
import { candidateAddress, connectionStats, httpStats } from "../../lib/modes";
import type { Mode, ProbeResult, PublishTarget, RunSummary } from "../../types";
import { PublicationStatus } from "../publish/PublishPage";

type SortKey = "score" | "tcp" | "https" | "bandwidth";
export interface CopyFields { country: boolean; tcpLatency: boolean; httpLatency: boolean; bandwidth: boolean }

const defaultCopyFields: CopyFields = { country: true, tcpLatency: false, httpLatency: true, bandwidth: true };
const percent = (value: number) => `${Math.round(value * 100)}%`;
const ms = (value: number) => `${value.toFixed(1)} ms`;
const keyOf = (item: ProbeResult) => `${candidateAddress(item.candidate)}:${item.candidate.port}`;
const compactNumber = (value: number) => value.toFixed(1).replace(/\.0$/, "");

export function formatCopyResults(results: ProbeResult[], fields: CopyFields) {
  return results.map((item) => {
    const country = fields.country && item.candidate.country ? `#${item.candidate.country}` : "";
    const values = [`${keyOf(item)}${item.mode==="h3"?`${country||"#"}${country?"|":""}H3`:country}`];
    if (fields.tcpLatency) values.push(`${item.mode==="h3"?"QUIC":"TCP"}${compactNumber(connectionStats(item).p95Ms)}ms`);
    if (fields.httpLatency) values.push(`${item.mode==="h3"?"HTTP3":"HTTP"}${compactNumber(httpStats(item).averageMs)}ms`);
    if (fields.bandwidth) values.push(`${compactNumber(item.bandwidth.mbps)}Mbps`);
    return values.join("|");
  }).join("\n");
}

export function formatCSVResults(results:ProbeResult[],mode:Mode="tcp_https"){
  const h3=mode==="h3";
  const header=h3?"Address,Port,Country,Mode,QUIC Success,QUIC P50,QUIC P95,HTTP3 Success,HTTP3 Latency,Jitter,Mbps,Score,Status,DNS Resolved At,IPv4 Addresses":"IP,Port,Country,TCP Success,TCP P50,TCP P95,HTTPS Success,HTTP Latency,Jitter,Mbps,Score,Status";
  const escape=(value:unknown)=>{const text=String(value??"");return /[",\r\n]/.test(text)?'"'+text.replaceAll('"','""')+'"':text;};
  return [header,...results.map(r=>[candidateAddress(r.candidate),r.candidate.port,r.candidate.country||"",...(h3?["h3"]:[]),connectionStats(r).successRate,connectionStats(r).p50Ms,connectionStats(r).p95Ms,httpStats(r).successRate,httpStats(r).averageMs,httpStats(r).jitterMs,r.bandwidth.mbps,r.score,r.status,...(h3?[r.resolvedAt??"",r.endpoints?.map(endpoint=>endpoint.ip).join(";")??r.candidate.ip]:[])].map(escape).join(","))].join("\n");
}

export const ResultsTable = memo(function ResultsTable({ summary }: { summary?: RunSummary }) {
  const settings = useAppStore((state) => state.settings);
  const isH3=summary?.mode==="h3",connectionLabel=isH3?"QUIC":"TCP",httpLabel=isH3?"HTTP/3":"HTTPS";
  const history = useAppStore((state) => state.history);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortKey>("score");
  const [selected, setSelected] = useState<ProbeResult>();
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [copyFields, setCopyFields] = useState<CopyFields>(defaultCopyFields);
  useEffect(()=>{setSelected(undefined);setSelectedKeys(new Set());},[summary?.runId]);
  const [savingRetention, setSavingRetention] = useState(false);
  const retainedCount = history.find((item) => item.state === "completed")?.results.length ?? 0;
  const results = useMemo(
    () => [...(summary?.results || [])]
      .filter((item) => `${keyOf(item)} ${item.candidate.country}`.toLowerCase().includes(query.toLowerCase()))
      .sort((a, b) => sort === "score" ? b.score - a.score : sort === "tcp" ? connectionStats(a).p95Ms - connectionStats(b).p95Ms : sort === "https" ? httpStats(a).p95Ms - httpStats(b).p95Ms : b.bandwidth.mbps - a.bandwidth.mbps),
    [summary, query, sort],
  );
  const output = selectedKeys.size ? results.filter((item) => selectedKeys.has(keyOf(item))) : results;
  const copy = async () => {
    await navigator.clipboard.writeText(formatCopyResults(output, copyFields));
    toast.success(`已复制 ${output.length} 个节点`);
  };
  const exportCSV = () => {
    const url = URL.createObjectURL(new Blob([formatCSVResults(output,isH3?"h3":"tcp_https")], { type: "text/csv;charset=utf-8" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `cf-node-bench-${Date.now()}.csv`;
    anchor.click();
    URL.revokeObjectURL(url);
  };
  const publish=async(target:"all"|PublishTarget)=>{
    if(!summary)return;
    try{await actions.publishRun(summary.runId,target);toast.success("发布任务已加入队列");}catch(reason){toast.danger(String(reason));}
  };
  const toggleRetention=async(value:boolean)=>{
    setSavingRetention(true);
    try{
      await actions.saveSettings({...settings,retainPreviousResults:value});
      toast.success(value?`全量测速时将优先复检上轮的 ${retainedCount} 个结果`:"已关闭全量测速的上轮复检");
    }catch(reason){
      toast.danger(`保存失败：${String(reason)}`);
    }finally{
      setSavingRetention(false);
    }
  };

  return <section className="panel results-panel">
    <div className="section-heading"><h2>测速结果{summary?` · ${isH3?"H3":"TCP/HTTPS"}`:""}</h2><span>{summary?.state === "cancelled" ? "任务已取消" : summary ? `完成于 ${new Date(summary.finishedAt).toLocaleTimeString("zh-CN")}` : "等待首次测速"}</span></div>
    {isH3&&<p className="h3-context">{results.length?"H3 入口已验证 · IPv4":"检测目标：H3 入口 · IPv4"}{summary?.workerHost?` · ${summary.workerHost}`:""}。域名按本轮全部解析 IP 的较差成绩排名；不代表完整 XHTTP、ECH 或 ProxyIP 出站已验证。</p>}
    {summary?.state==="completed"&&<div className="publication-strip" aria-label="发布状态">{summary.publications.length?summary.publications.map(result=><div className="publication-item" key={result.target}><strong>{{cloudflare:"Cloudflare",github:"GitHub 仓库",gist:"Gist",telegram:"Telegram"}[result.target]}</strong><PublicationStatus result={result}/><span className="spacer"/><Button isIconOnly variant="tertiary" aria-label={`重试 ${result.target}`} isDisabled={result.state==="queued"||result.state==="running"} onPress={()=>void publish(result.target)}><RotateCw size={14}/></Button></div>):<><span className="results-summary">尚无发布记录</span><span className="spacer"/><Button variant="tertiary" onPress={()=>void publish("all")}><Send size={14}/>发布结果</Button></>}</div>}
    <div className="results-toolbar">
      <Input className="search" aria-label="筛选节点" placeholder="筛选 IP、域名、端口或国家" value={query} onChange={(event) => setQuery(event.target.value)} />
      <Button variant="secondary" onPress={() => setSort(sort === "score" ? "tcp" : sort === "tcp" ? "https" : sort === "https" ? "bandwidth" : "score")}>排序：{{ score: "综合分", tcp: `${connectionLabel} P95`, https: `${httpLabel} P95`, bandwidth: "带宽" }[sort]}</Button>
      {retainedCount>0&&<Switch className="retention-switch" aria-label={`全量测速时优先复检上轮结果，${retainedCount} 个节点`} isSelected={settings.retainPreviousResults} isDisabled={savingRetention} onChange={value=>void toggleRetention(value)}><Switch.Content><Switch.Control><Switch.Thumb/></Switch.Control><span>全量测速时复检上轮（{retainedCount}）</span></Switch.Content></Switch>}
      <span className="spacer" /><span className="results-summary">{selectedKeys.size ? `已选 ${selectedKeys.size}` : `${results.length} 个节点`}</span>
      <Popover><Popover.Trigger><Button isIconOnly variant="tertiary" aria-label="复制选项" isDisabled={!results.length}><SlidersHorizontal size={15} /></Button></Popover.Trigger><Popover.Content isNonModal placement="bottom end"><Popover.Dialog className="copy-options"><Popover.Heading>复制内容</Popover.Heading><div className="copy-options-list">
        <CopyOption label="国家代码" selected={copyFields.country} onChange={(country) => setCopyFields((fields) => ({ ...fields, country }))} />
        <CopyOption label={`${connectionLabel} P95`} selected={copyFields.tcpLatency} onChange={(tcpLatency) => setCopyFields((fields) => ({ ...fields, tcpLatency }))} />
        <CopyOption label={`${isH3?"HTTP/3":"HTTP"} 平均延迟`} selected={copyFields.httpLatency} onChange={(httpLatency) => setCopyFields((fields) => ({ ...fields, httpLatency }))} />
        <CopyOption label="下载带宽" selected={copyFields.bandwidth} onChange={(bandwidth) => setCopyFields((fields) => ({ ...fields, bandwidth }))} />
      </div></Popover.Dialog></Popover.Content></Popover>
      <span title="复制结果"><Button isIconOnly variant="tertiary" aria-label="复制结果" isDisabled={!results.length} onPress={() => void copy()}><Clipboard size={15} /></Button></span>
      <span title="导出 CSV"><Button isIconOnly variant="tertiary" aria-label="导出 CSV" isDisabled={!results.length} onPress={exportCSV}><Download size={15} /></Button></span>
    </div>
    {results.length ? <Table variant="secondary"><Table.ScrollContainer><Table.Content aria-label="Cloudflare 节点测速结果" selectionMode="multiple" selectedKeys={selectedKeys} onSelectionChange={(keys) => setSelectedKeys(keys === "all" ? new Set(results.map(keyOf)) : new Set([...keys].map(String)))}>
      <Table.Header>
        <Table.Column id="selection"><Checkbox slot="selection" aria-label="选择全部" isSelected={selectedKeys.size === results.length} isIndeterminate={selectedKeys.size > 0 && selectedKeys.size < results.length} onChange={(checked) => setSelectedKeys(checked ? new Set(results.map(keyOf)) : new Set())}><Checkbox.Control><Checkbox.Indicator /></Checkbox.Control></Checkbox></Table.Column>
        <Table.Column id="ip" isRowHeader>{isH3?"地址 / 端口":"IP / 端口"}</Table.Column><Table.Column id="country">国家</Table.Column><Table.Column id="tcpSuccess">{`${connectionLabel} 成功`}</Table.Column><Table.Column id="tcpP50">{`${connectionLabel} P50`}</Table.Column><Table.Column id="tcpP95">{`${connectionLabel} P95`}</Table.Column><Table.Column id="httpsSuccess">{`${httpLabel} 成功`}</Table.Column><Table.Column id="http">{`${isH3?"HTTP/3":"HTTP"} 延迟`}</Table.Column><Table.Column id="jitter">抖动</Table.Column><Table.Column id="bandwidth">下载带宽</Table.Column><Table.Column id="score">综合分</Table.Column><Table.Column id="status">状态</Table.Column>
      </Table.Header>
      <Table.Body>{results.map((result) => <Table.Row id={keyOf(result)} key={keyOf(result)} onAction={() => setSelected(result)}>
        <Table.Cell><Checkbox slot="selection" aria-label={`选择 ${keyOf(result)}`}><Checkbox.Control><Checkbox.Indicator /></Checkbox.Control></Checkbox></Table.Cell>
        <Table.Cell><span className="ip-cell">{keyOf(result)}</span></Table.Cell><Table.Cell>{result.candidate.country || "-"}</Table.Cell><Table.Cell>{percent(connectionStats(result).successRate)}</Table.Cell><Table.Cell>{ms(connectionStats(result).p50Ms)}</Table.Cell><Table.Cell>{ms(connectionStats(result).p95Ms)}</Table.Cell><Table.Cell>{percent(httpStats(result).successRate)}</Table.Cell><Table.Cell>{ms(httpStats(result).averageMs)}</Table.Cell><Table.Cell>{ms(httpStats(result).jitterMs)}</Table.Cell><Table.Cell><span className="metric-good">{result.bandwidth.mbps.toFixed(1)} Mbps</span></Table.Cell><Table.Cell><span className="metric-good">{result.score.toFixed(1)}</span></Table.Cell><Table.Cell><span className="status-pill">通过</span></Table.Cell>
      </Table.Row>)}</Table.Body>
    </Table.Content></Table.ScrollContainer></Table> : <div className="empty-results"><div><Search size={26} /><strong>{summary?.state === "cancelled" ? "测速已取消" : "暂无结果"}</strong><span>{summary ? "当前条件下没有合格节点" : "开始测速后，合格节点将直接显示在这里"}</span></div></div>}
    {selected && <Drawer isOpen onOpenChange={(open) => { if (!open) setSelected(undefined); }}><Drawer.Trigger className="sr-only" aria-label="节点明细"><span /></Drawer.Trigger><Drawer.Backdrop><Drawer.Content className="result-drawer" placement="right"><Drawer.Dialog><Drawer.Header><Drawer.Heading>{keyOf(selected)}</Drawer.Heading><Drawer.CloseTrigger aria-label="关闭"><X size={17} /></Drawer.CloseTrigger></Drawer.Header><Drawer.Body>
      <p className="source-meta">{selected.candidate.country || "未知国家"} · {selected.candidate.addressType.toUpperCase()} · {selected.candidate.sourceId}</p>
      <div className="drawer-grid"><Metric label={`${connectionLabel} 成功率`} value={percent(connectionStats(selected).successRate)} /><Metric label={`${connectionLabel} P50 / P95`} value={`${ms(connectionStats(selected).p50Ms)} / ${ms(connectionStats(selected).p95Ms)}`} /><Metric label={`${httpLabel} 成功率`} value={percent(httpStats(selected).successRate)} /><Metric label={`${isH3?"HTTP/3":"HTTP"} 平均延迟`} value={ms(httpStats(selected).averageMs)} /><Metric label={`${httpLabel} 抖动`} value={ms(httpStats(selected).jitterMs)} /><Metric label="下载带宽" value={`${selected.bandwidth.mbps.toFixed(1)} Mbps`} /><Metric label="首字节" value={ms(selected.bandwidth.ttfbMs)} /><Metric label="实际下载" value={`${(selected.bandwidth.bytes / 1048576).toFixed(1)} MiB`} /></div>
      {isH3&&<section className="endpoint-details" aria-label="逐 IP 检测明细"><h3>本轮 IPv4 明细</h3><p className="source-meta">{selected.candidate.addressType==="domain"?"DNS 解析时间：":"检测时间："}{selected.resolvedAt?new Date(selected.resolvedAt).toLocaleString("zh-CN"):"未记录"}。后续 DNS 变化及 IPv6 不在本轮验证范围内。</p>{selected.endpoints?.map(endpoint=><article key={endpoint.ip}><h4>{endpoint.ip}:{selected.candidate.port}</h4><div className="drawer-grid"><Metric label="QUIC 成功率" value={percent(endpoint.quic.successRate)}/><Metric label="QUIC P95" value={ms(endpoint.quic.p95Ms)}/><Metric label="HTTP/3 成功率" value={percent(endpoint.http3.successRate)}/><Metric label="HTTP/3 P95" value={ms(endpoint.http3.p95Ms)}/><Metric label="下载带宽" value={endpoint.bandwidth.mbps.toFixed(1)+" Mbps"}/><Metric label="实际下载" value={(endpoint.bandwidth.bytes/1048576).toFixed(1)+" MiB"}/></div></article>)}</section>}
      <div className="score-breakdown"><h3>评分明细 · {selected.score.toFixed(1)}</h3>{([
        [connectionLabel,isH3?selected.parts.quic??0:selected.parts.tcp,summary?connectionWeight(summary.scoreWeights,summary.mode):0],
        [httpLabel,isH3?selected.parts.http3??0:selected.parts.https,summary?httpWeight(summary.scoreWeights,summary.mode):0],
        ["抖动",selected.parts.jitter,summary?.scoreWeights.jitter??0],
        ["可用性",selected.parts.reliability,summary?.scoreWeights.reliability??0],
        ["带宽",selected.parts.bandwidth,summary?.scoreWeights.bandwidth??0],
      ] as [string,number,number][]).map(([label,value,weight]) => <div className="score-row" key={label}><span>{label}</span><div className="score-track"><div className="score-fill" style={{ width: `${value}%` }} /></div><b>{value.toFixed(0)} × {weight}% = {(value*weight/100).toFixed(1)}</b></div>)}</div>
    </Drawer.Body></Drawer.Dialog></Drawer.Content></Drawer.Backdrop></Drawer>}
  </section>;
});

function Metric({ label, value }: { label: string; value: string }) {
  return <div className="drawer-metric"><label>{label}</label><strong>{value}</strong></div>;
}

function CopyOption({ label, selected, onChange }: { label: string; selected: boolean; onChange: (value: boolean) => void }) {
  return <Checkbox isSelected={selected} onChange={onChange}><Checkbox.Content><Checkbox.Control><Checkbox.Indicator /></Checkbox.Control>{label}</Checkbox.Content></Checkbox>;
}
