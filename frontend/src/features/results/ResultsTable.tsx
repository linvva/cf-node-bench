import { useMemo, useState } from "react";
import { Button, Checkbox, Drawer, Input, Popover, Table, toast } from "@heroui/react";
import { Clipboard, Download, Search, SlidersHorizontal, X } from "lucide-react";
import type { ProbeResult, RunSummary } from "../../types";

type SortKey = "score" | "tcp" | "https" | "bandwidth";
export interface CopyFields { country: boolean; tcpLatency: boolean; httpLatency: boolean; bandwidth: boolean }

const defaultCopyFields: CopyFields = { country: true, tcpLatency: false, httpLatency: false, bandwidth: false };
const percent = (value: number) => `${Math.round(value * 100)}%`;
const ms = (value: number) => `${value.toFixed(1)} ms`;
const keyOf = (item: ProbeResult) => `${item.candidate.ip}:${item.candidate.port}`;

export function formatCopyResults(results: ProbeResult[], fields: CopyFields) {
  return results.map((item) => {
    const country = fields.country && item.candidate.country ? `#${item.candidate.country}` : "";
    const values = [`${keyOf(item)}${country}`];
    if (fields.tcpLatency) values.push(`TCP ${item.tcp.p95Ms.toFixed(1)} ms`);
    if (fields.httpLatency) values.push(`HTTP ${item.https.averageMs.toFixed(1)} ms`);
    if (fields.bandwidth) values.push(`${item.bandwidth.mbps.toFixed(1)} Mbps`);
    return values.join("\t");
  }).join("\n");
}

export function ResultsTable({ summary }: { summary?: RunSummary }) {
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortKey>("score");
  const [selected, setSelected] = useState<ProbeResult>();
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [copyFields, setCopyFields] = useState<CopyFields>(defaultCopyFields);
  const results = useMemo(
    () => [...(summary?.results || [])]
      .filter((item) => `${keyOf(item)} ${item.candidate.country}`.toLowerCase().includes(query.toLowerCase()))
      .sort((a, b) => sort === "score" ? b.score - a.score : sort === "tcp" ? a.tcp.p95Ms - b.tcp.p95Ms : sort === "https" ? a.https.p95Ms - b.https.p95Ms : b.bandwidth.mbps - a.bandwidth.mbps),
    [summary, query, sort],
  );
  const output = selectedKeys.size ? results.filter((item) => selectedKeys.has(keyOf(item))) : results;
  const copy = async () => {
    await navigator.clipboard.writeText(formatCopyResults(output, copyFields));
    toast.success(`已复制 ${output.length} 个节点`);
  };
  const exportCSV = () => {
    const header = "IP,Port,Country,TCP Success,TCP P50,TCP P95,HTTPS Success,HTTP Latency,Jitter,Mbps,Score,Status";
    const lines = output.map((r) => [r.candidate.ip, r.candidate.port, r.candidate.country || "", r.tcp.successRate, r.tcp.p50Ms, r.tcp.p95Ms, r.https.successRate, r.https.averageMs, r.https.jitterMs, r.bandwidth.mbps, r.score, r.status].join(","));
    const url = URL.createObjectURL(new Blob([[header, ...lines].join("\n")], { type: "text/csv;charset=utf-8" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `cf-node-bench-${Date.now()}.csv`;
    anchor.click();
    URL.revokeObjectURL(url);
  };

  return <section className="panel results-panel">
    <div className="section-heading"><h2>测速结果</h2><span>{summary?.state === "cancelled" ? "任务已取消" : summary ? `完成于 ${new Date(summary.finishedAt).toLocaleTimeString("zh-CN")}` : "等待首次测速"}</span></div>
    <div className="results-toolbar">
      <Input className="search" aria-label="筛选节点" placeholder="筛选 IP、端口或国家" value={query} onChange={(event) => setQuery(event.target.value)} />
      <Button variant="secondary" onPress={() => setSort(sort === "score" ? "tcp" : sort === "tcp" ? "https" : sort === "https" ? "bandwidth" : "score")}>排序：{{ score: "综合分", tcp: "TCP P95", https: "HTTPS P95", bandwidth: "带宽" }[sort]}</Button>
      <span className="spacer" /><span className="results-summary">{selectedKeys.size ? `已选 ${selectedKeys.size}` : `${results.length} 个节点`}</span>
      <Popover><Popover.Trigger><Button isIconOnly variant="tertiary" aria-label="复制选项" isDisabled={!results.length}><SlidersHorizontal size={15} /></Button></Popover.Trigger><Popover.Content isNonModal placement="bottom end"><Popover.Dialog className="copy-options"><Popover.Heading>复制内容</Popover.Heading><div className="copy-options-list">
        <CopyOption label="国家代码" selected={copyFields.country} onChange={(country) => setCopyFields((fields) => ({ ...fields, country }))} />
        <CopyOption label="TCP P95" selected={copyFields.tcpLatency} onChange={(tcpLatency) => setCopyFields((fields) => ({ ...fields, tcpLatency }))} />
        <CopyOption label="HTTP 平均延迟" selected={copyFields.httpLatency} onChange={(httpLatency) => setCopyFields((fields) => ({ ...fields, httpLatency }))} />
        <CopyOption label="下载带宽" selected={copyFields.bandwidth} onChange={(bandwidth) => setCopyFields((fields) => ({ ...fields, bandwidth }))} />
      </div></Popover.Dialog></Popover.Content></Popover>
      <span title="复制结果"><Button isIconOnly variant="tertiary" aria-label="复制结果" isDisabled={!results.length} onPress={() => void copy()}><Clipboard size={15} /></Button></span>
      <span title="导出 CSV"><Button isIconOnly variant="tertiary" aria-label="导出 CSV" isDisabled={!results.length} onPress={exportCSV}><Download size={15} /></Button></span>
    </div>
    {results.length ? <Table variant="secondary"><Table.ScrollContainer><Table.Content aria-label="Cloudflare 节点测速结果" selectionMode="multiple" selectedKeys={selectedKeys} onSelectionChange={(keys) => setSelectedKeys(keys === "all" ? new Set(results.map(keyOf)) : new Set([...keys].map(String)))}>
      <Table.Header>
        <Table.Column id="selection"><Checkbox slot="selection" aria-label="选择全部" isSelected={selectedKeys.size === results.length} isIndeterminate={selectedKeys.size > 0 && selectedKeys.size < results.length} onChange={(checked) => setSelectedKeys(checked ? new Set(results.map(keyOf)) : new Set())}><Checkbox.Control><Checkbox.Indicator /></Checkbox.Control></Checkbox></Table.Column>
        <Table.Column id="ip" isRowHeader>IP / 端口</Table.Column><Table.Column id="country">国家</Table.Column><Table.Column id="tcpSuccess">TCP 成功</Table.Column><Table.Column id="tcpP50">TCP P50</Table.Column><Table.Column id="tcpP95">TCP P95</Table.Column><Table.Column id="httpsSuccess">HTTPS 成功</Table.Column><Table.Column id="http">HTTP 延迟</Table.Column><Table.Column id="jitter">抖动</Table.Column><Table.Column id="bandwidth">下载带宽</Table.Column><Table.Column id="score">综合分</Table.Column><Table.Column id="status">状态</Table.Column>
      </Table.Header>
      <Table.Body>{results.map((result) => <Table.Row id={keyOf(result)} key={keyOf(result)} onAction={() => setSelected(result)}>
        <Table.Cell><Checkbox slot="selection" aria-label={`选择 ${keyOf(result)}`}><Checkbox.Control><Checkbox.Indicator /></Checkbox.Control></Checkbox></Table.Cell>
        <Table.Cell><span className="ip-cell">{keyOf(result)}</span></Table.Cell><Table.Cell>{result.candidate.country || "-"}</Table.Cell><Table.Cell>{percent(result.tcp.successRate)}</Table.Cell><Table.Cell>{ms(result.tcp.p50Ms)}</Table.Cell><Table.Cell>{ms(result.tcp.p95Ms)}</Table.Cell><Table.Cell>{percent(result.https.successRate)}</Table.Cell><Table.Cell>{ms(result.https.averageMs)}</Table.Cell><Table.Cell>{ms(result.https.jitterMs)}</Table.Cell><Table.Cell><span className="metric-good">{result.bandwidth.mbps.toFixed(1)} Mbps</span></Table.Cell><Table.Cell><span className="metric-good">{result.score.toFixed(1)}</span></Table.Cell><Table.Cell><span className="status-pill">通过</span></Table.Cell>
      </Table.Row>)}</Table.Body>
    </Table.Content></Table.ScrollContainer></Table> : <div className="empty-results"><div><Search size={26} /><strong>{summary?.state === "cancelled" ? "测速已取消" : "暂无结果"}</strong><span>{summary ? "当前条件下没有合格节点" : "开始测速后，合格节点将直接显示在这里"}</span></div></div>}
    {selected && <Drawer isOpen onOpenChange={(open) => { if (!open) setSelected(undefined); }}><Drawer.Trigger className="sr-only" aria-label="节点明细"><span /></Drawer.Trigger><Drawer.Backdrop><Drawer.Content className="result-drawer" placement="right"><Drawer.Dialog><Drawer.Header><Drawer.Heading>{keyOf(selected)}</Drawer.Heading><Drawer.CloseTrigger aria-label="关闭"><X size={17} /></Drawer.CloseTrigger></Drawer.Header><Drawer.Body>
      <p className="source-meta">{selected.candidate.country || "未知国家"} · {selected.candidate.addressType.toUpperCase()} · {selected.candidate.sourceId}</p>
      <div className="drawer-grid"><Metric label="TCP 成功率" value={percent(selected.tcp.successRate)} /><Metric label="TCP P50 / P95" value={`${ms(selected.tcp.p50Ms)} / ${ms(selected.tcp.p95Ms)}`} /><Metric label="HTTPS 成功率" value={percent(selected.https.successRate)} /><Metric label="HTTP 平均延迟" value={ms(selected.https.averageMs)} /><Metric label="HTTPS 抖动" value={ms(selected.https.jitterMs)} /><Metric label="下载带宽" value={`${selected.bandwidth.mbps.toFixed(1)} Mbps`} /><Metric label="首字节" value={ms(selected.bandwidth.ttfbMs)} /><Metric label="实际下载" value={`${(selected.bandwidth.bytes / 1048576).toFixed(1)} MiB`} /></div>
      <div className="score-breakdown"><h3>评分明细 · {selected.score.toFixed(1)}</h3>{Object.entries({ TCP: selected.parts.tcp, HTTPS: selected.parts.https, 抖动: selected.parts.jitter, 可用性: selected.parts.reliability, 带宽: selected.parts.bandwidth }).map(([label, value]) => <div className="score-row" key={label}><span>{label}</span><div className="score-track"><div className="score-fill" style={{ width: `${value}%` }} /></div><b>{value.toFixed(0)}</b></div>)}</div>
    </Drawer.Body></Drawer.Dialog></Drawer.Content></Drawer.Backdrop></Drawer>}
  </section>;
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div className="drawer-metric"><label>{label}</label><strong>{value}</strong></div>;
}

function CopyOption({ label, selected, onChange }: { label: string; selected: boolean; onChange: (value: boolean) => void }) {
  return <Checkbox isSelected={selected} onChange={onChange}><Checkbox.Content><Checkbox.Control><Checkbox.Indicator /></Checkbox.Control>{label}</Checkbox.Content></Checkbox>;
}
