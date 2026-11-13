import { useEffect, useState } from "react";
import { Button, Heading, Input, Label, ListBox, Select, Slider, toast } from "@heroui/react";
import { RotateCcw, Save } from "lucide-react";
import { bandwidthTestURLPresets, h3BandwidthTestURLPresets } from "../../lib/bandwidthTestUrls";
import { DEFAULT_SCORE_WEIGHTS, scoreWeightTotal } from "../../lib/scoring";
import { actions, useAppStore } from "../../store";
import type { ScoreWeights, Settings } from "../../types";
import { CountryPicker } from "./CountryPicker";

const constraints = {
  connectionConcurrency:[1,256,"连接并发数"],httpConcurrency:[1,64,"HTTP 并发数"],bandwidthConcurrency:[1,10,"带宽并发数"],
  connectionProbeCount:[1,10,"连接探测次数"],httpProbeCount:[1,10,"HTTP 探测次数"],httpCandidateCount:[1,5000,"HTTP 候选数"],
  connectTimeoutMs:[100,30000,"连接超时"],requestTimeoutMs:[500,60000,"HTTPS 超时"],h3ConnectTimeoutMs:[100,30000,"QUIC 超时"],h3RequestTimeoutMs:[500,60000,"HTTP/3 超时"],
  bandwidthTimeoutMs:[1000,120000,"带宽总超时"],sourceTimeoutMs:[500,60000,"数据源超时"],sourceRetries:[0,3,"数据源重试次数"],bandwidthCandidates:[1,500,"带宽目标通过数"],finalResultCount:[1,100,"最终结果数"],
} satisfies Record<string,[number,number,string]>;
type IntegerSetting=keyof typeof constraints;

export function SettingsPage() {
  const saved = useAppStore((state) => state.settings);
  const [form, setForm] = useState(saved);
  const connectionLabel="连接",httpLabel="HTTP";
  const [error, setError] = useState("");

  useEffect(() => setForm(saved), [saved]);

  const number = (key: IntegerSetting, value: string) => setForm((current) => ({ ...current, [key]: Number(value) }));
  const save = async () => {
    const normalized = { ...form, verificationHost:form.verificationHost.trim().toLowerCase().replace(/\.$/,""),bandwidthTestUrl:form.bandwidthTestUrl.trim(),h3BandwidthTestUrl:form.h3BandwidthTestUrl.trim() };
    const message = validateSettings(normalized);
    setError(message);
    if (message) return;
    setForm(normalized);
    try{await actions.saveSettings(normalized);toast.success("设置已保存");}catch(reason){setError(String(reason));}
  };

  return <div className="page" data-testid="settings-page">
    <header className="page-header"><div><h1>测速设置</h1><p>普通和 H3 模式共用并发、采样和评分权重；在工作台选择测速模式。</p></div><div className="page-action-area"><div className="page-actions"><Button variant="primary" onPress={() => void save()}><Save size={15} />保存设置</Button></div><p className="page-action-status" role={error?"alert":undefined} title={error}>{error||"\u00a0"}</p></div></header>
    <div className="settings-layout">
      <div className="settings-column">

        <SettingsSection title="并发" description="并发越高不一定越快，可能受本机网络和系统句柄限制。">
          <Field label="连接并发" value={form.connectionConcurrency} min={1} max={256} onChange={(value) => number("connectionConcurrency", value)} />
          <Field label={`${httpLabel} 并发`} value={form.httpConcurrency} min={1} max={64} onChange={(value) => number("httpConcurrency", value)} />
          <Field label="带宽并发" value={form.bandwidthConcurrency} min={1} max={10} onChange={(value) => number("bandwidthConcurrency", value)} />
        </SettingsSection>
        <SettingsSection title="候选池与过滤" description="连接合格的节点全部完成 HTTP 检测，再按成功率、P95 延迟选出候选池。">
          <Field label="HTTP 候选池" value={form.httpCandidateCount} min={1} max={5000} onChange={(value) => number("httpCandidateCount", value)} />
          <Field label="带宽目标通过数" value={form.bandwidthCandidates} min={1} max={500} onChange={(value) => number("bandwidthCandidates", value)} />
          <Field label="最终结果数" value={form.finalResultCount} min={1} max={100} onChange={(value) => number("finalResultCount", value)} />
          <div className="field field-wide"><small>带宽按目标数量首次排队，再按通过率批量补测；达到目标后等待在测任务完成，可能略超目标。空白过滤列表表示不限，排除国家优先。</small></div>
          <div className="field field-wide"><label>允许端口</label><Input aria-label="允许端口" value={form.allowedPorts.join(", ")} onChange={(event) => setForm((current) => ({ ...current, allowedPorts: event.target.value.split(/[,\s]+/).filter(Boolean).map(Number) }))} /><small>逗号分隔；留空表示不限</small></div>
          <CountryPicker label="允许国家" value={form.allowedCountries} description="留空表示不限；支持按中文名称或代码搜索" onChange={(value) => setForm((current) => ({ ...current, allowedCountries: value }))} />
          <CountryPicker label="排除国家" value={form.blockedCountries} description={`仅匹配数据源提供的国家标签；命中后在 ${connectionLabel} 前排除并显示数量`} onChange={(value) => setForm((current) => ({ ...current, blockedCountries: value }))} />
        </SettingsSection>
        <SettingsSection title="下载测速" description="普通测速使用下方地址；H3 下载地址在高级设置中配置。下载上限按每个实际 IP 计算。">
          <BandwidthTestPresetField isH3={false} value={form.bandwidthTestUrl} onChange={(value) => setForm((current) => ({ ...current, bandwidthTestUrl: value }))} />
          <MiBField value={form.maxDownloadBytes} onChange={(value) => setForm((current) => ({ ...current, maxDownloadBytes: value }))} />
          <div className="field field-wide"><label>下载测速 URL</label><Input aria-label="下载测速 URL" value={form.bandwidthTestUrl} placeholder="https://example.com/file.bin" onChange={(event) => setForm((current) => ({ ...current, bandwidthTestUrl: event.target.value }))} /></div>
        </SettingsSection>
      </div>
      <div className="settings-column">
        <SettingsSection title="超时与数据源" description="数据源失败会按固定短间隔重试，取消任务会立即中断等待。">
          <Field label="连接超时 (ms)" value={form.connectTimeoutMs} min={100} max={30000} onChange={(value) => number("connectTimeoutMs", value)} />
          <Field label="HTTPS 超时 (ms)" value={form.requestTimeoutMs} min={500} max={60000} onChange={(value) => number("requestTimeoutMs", value)} />
          <Field label="带宽总超时 (ms)" value={form.bandwidthTimeoutMs} min={1000} max={120000} onChange={(value) => number("bandwidthTimeoutMs", value)} />
          <Field label="数据源超时 (ms)" value={form.sourceTimeoutMs} min={500} max={60000} onChange={(value) => number("sourceTimeoutMs", value)} />
          <Field label="数据源重试次数" value={form.sourceRetries} min={0} max={3} onChange={(value) => number("sourceRetries", value)} />
        </SettingsSection>
        <SettingsSection title="采样与可用性" description={`${connectionLabel} 和 ${httpLabel} 分别通过硬门槛后，才会进入后续阶段。`}>
          <Field label="连接探测次数" value={form.connectionProbeCount} min={1} max={10} onChange={(value) => number("connectionProbeCount", value)} />
          <Field label={`${httpLabel} 探测次数`} value={form.httpProbeCount} min={1} max={10} onChange={(value) => number("httpProbeCount", value)} />
          <RateField label="连接最低成功率" value={form.connectionMinSuccessRate} onChange={(value) => setForm((current) => ({ ...current, ["connectionMinSuccessRate"]: value }))} />
          <RateField label={`${httpLabel} 最低成功率`} value={form.httpMinSuccessRate} onChange={(value) => setForm((current) => ({ ...current, ["httpMinSuccessRate"]: value }))} />
        </SettingsSection>
        <SettingsSection title="评分权重" description="通过硬门槛的节点先归一化，再按以下权重计算综合分。">
          <div className="score-weight-summary field-wide"><span>当前总和</span><strong data-valid={scoreWeightTotal(form.scoreWeights)===100}>{scoreWeightTotal(form.scoreWeights)}%</strong><Button variant="tertiary" onPress={()=>setForm(current=>({...current,scoreWeights:{...DEFAULT_SCORE_WEIGHTS}}))}><RotateCcw size={14}/>恢复默认</Button></div>
          <WeightField label="带宽" value={form.scoreWeights.bandwidth} onChange={bandwidth=>setForm(current=>({...current,scoreWeights:{...current.scoreWeights,bandwidth}}))}/>
          <WeightField label={httpLabel} value={form.scoreWeights.http??0} onChange={value=>setForm(current=>({...current,scoreWeights:{...current.scoreWeights,["http"]:value}}))}/>
          <WeightField label={connectionLabel} value={form.scoreWeights.connection??0} onChange={value=>setForm(current=>({...current,scoreWeights:{...current.scoreWeights,["connection"]:value}}))}/>
          <WeightField label="可用性" value={form.scoreWeights.reliability} onChange={reliability=>setForm(current=>({...current,scoreWeights:{...current.scoreWeights,reliability}}))}/>
          <WeightField label="抖动" value={form.scoreWeights.jitter} onChange={jitter=>setForm(current=>({...current,scoreWeights:{...current.scoreWeights,jitter}}))}/>
        </SettingsSection>
      </div>
    </div>
    <details className="panel h3-advanced"><summary>H3 高级设置</summary><SettingsSection title="HTTP/3 入口验证" description="默认验证 Cloudflare 公共域名，无需 Worker 或凭据。自定义域名只验证该域名入口，不验证完整 XHTTP、ECH 或 ProxyIP。">
      <div className="field field-wide"><label>验证域名（可选）</label><Input aria-label="验证域名" value={form.verificationHost} placeholder="h3.speed.cloudflare.com" onChange={event=>setForm(current=>({...current,verificationHost:event.target.value}))}/><small>留空使用 h3.speed.cloudflare.com；更改后需重新探测连接。</small></div>
      <Field label="QUIC 握手超时 (ms)" value={form.h3ConnectTimeoutMs} min={100} max={30000} onChange={value=>number("h3ConnectTimeoutMs",value)}/>
      <Field label="HTTP/3 超时 (ms)" value={form.h3RequestTimeoutMs} min={500} max={60000} onChange={value=>number("h3RequestTimeoutMs",value)}/>
      <BandwidthTestPresetField isH3 value={form.h3BandwidthTestUrl} onChange={h3BandwidthTestUrl=>setForm(current=>({...current,h3BandwidthTestUrl}))}/>
      <div className="field field-wide"><label>H3 下载测速 URL</label><Input aria-label="H3 下载测速 URL" value={form.h3BandwidthTestUrl} onChange={event=>setForm(current=>({...current,h3BandwidthTestUrl:event.target.value}))}/><small>固定候选 IPv4，强制 HTTP/3，不跟随重定向；多 IP 域名累计下载。</small></div>
    </SettingsSection></details>
  </div>;
}

export function validateSettings(form: Settings) {
    if(form.verificationHost.trim()&&!validWorkerHost(form.verificationHost))return "请填写有效的验证域名（不含协议、路径或端口），也可留空";
    for(const [key,[min,max,label]] of Object.entries(constraints) as [IntegerSetting,[number,number,string]][]){const value=form[key];if(!Number.isInteger(value)||value<min||value>max)return `${label}必须在 ${min} 到 ${max} 之间`;}
    if(!Number.isFinite(form.connectionMinSuccessRate)||form.connectionMinSuccessRate<0.6||form.connectionMinSuccessRate>1)return "连接最低成功率必须在 60% 到 100% 之间";
    if(!Number.isFinite(form.httpMinSuccessRate)||form.httpMinSuccessRate<0.6||form.httpMinSuccessRate>1)return "HTTP 最低成功率必须在 60% 到 100% 之间";
    for(const [label,value] of Object.entries(form.scoreWeights) as [keyof ScoreWeights,number][]){
      if(!Number.isInteger(value)||value<0||value>100)return `${{connection:"连接",http:"HTTP",tcp:"TCP",https:"HTTPS",quic:"QUIC",http3:"HTTP/3",jitter:"抖动",reliability:"可用性",bandwidth:"带宽"}[label]}权重必须在 0 到 100 之间`;
    }
    if(scoreWeightTotal(form.scoreWeights)!==100)return "评分权重总和必须等于 100";
    if (form.maxDownloadBytes < 1048576 || form.maxDownloadBytes > 1073741824) return "最大下载量必须在 1 到 1024 MiB 之间";
    const bandwidthURLMessage = validateBandwidthTestURL(form.bandwidthTestUrl)||validateBandwidthTestURL(form.h3BandwidthTestUrl);
    if (bandwidthURLMessage) return bandwidthURLMessage;
    if(form.bandwidthCandidates>form.httpCandidateCount)return "带宽目标通过数不能大于 HTTP 候选数";
    if (form.finalResultCount > form.bandwidthCandidates) return "最终结果数不能大于带宽目标通过数";
    if (form.allowedPorts.some((port) => !Number.isInteger(port) || port < 1 || port > 65535)) return "端口必须在 1 到 65535 之间";
    if ([...form.allowedCountries, ...form.blockedCountries].some((country) => !/^[A-Z]{2}$/.test(country))) return "国家代码必须是两个大写字母";
    const blocked = new Set(form.blockedCountries);
    const conflict = form.allowedCountries.find((country) => blocked.has(country));
    if (conflict) return `国家 ${conflict} 不能同时出现在允许和排除列表`;
    return "";
}

export function validWorkerHost(value:string){const host=value.trim().toLowerCase().replace(/\.$/,"");return host.length<=253&&host.includes(".")&&!/^\d+(?:\.\d+){3}$/.test(host)&&host.split(".").every(label=>/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label));}

function WeightField({label,value,onChange}:{label:string;value:number;onChange:(value:number)=>void}){
  return <div className="field field-wide score-weight-field"><div className="score-weight-label"><label>{label}</label><Input type="number" aria-label={`${label}权重数值`} value={String(value)} min={0} max={100} onChange={event=>onChange(Number(event.target.value))}/></div><Slider aria-label={`${label}权重`} value={value} minValue={0} maxValue={100} step={1} onChange={next=>onChange(Number(next))}><Slider.Track><Slider.Fill/><Slider.Thumb/></Slider.Track></Slider></div>;
}

export function validateBandwidthTestURL(value: string) {
    const raw = value.trim();
    if (!raw) return "下载测速地址不能为空";
    let target: URL;
    try { target = new URL(raw); } catch { return "下载测速地址必须是完整的 HTTPS URL"; }
    if (!raw.toLowerCase().startsWith("https://") || target.protocol !== "https:" || !target.hostname) return "下载测速地址必须是完整的 HTTPS URL";
    if (target.username || target.password) return "下载测速地址不能包含用户名或密码";
    if (target.hash) return "下载测速地址不能包含片段";
    return "";
}

function SettingsSection({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return <section className="panel settings-section"><h2>{title}</h2><p>{description}</p><div className="field-grid">{children}</div></section>;
}

function Field({ label, value, min, max, onChange }: { label: string; value: number; min: number; max: number; onChange: (value: string) => void }) {
  return <div className="field"><label>{label}</label><Input type="number" aria-label={label} value={String(value)} min={min} max={max} onChange={(event) => onChange(event.target.value)} /><small>{min} – {max}</small></div>;
}

function RateField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  const percent = Math.round(value * 1000) / 10;
  return <div className="field"><label>{label} (%)</label><Input type="number" aria-label={label} value={String(percent)} min={60} max={100} step={0.1} onChange={(event) => onChange(Number(event.target.value) / 100)} /><small>60 – 100</small></div>;
}

function MiBField({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  return <div className="field"><label>最大下载量 (MiB)</label><Input type="number" aria-label="最大下载量" value={String(value / 1048576)} min={1} max={1024} onChange={(event) => onChange(Math.round(Number(event.target.value) * 1048576))} /><small>1 – 1024</small></div>;
}

function BandwidthTestPresetField({ value, onChange, isH3 }: { value: string; onChange: (value: string) => void; isH3: boolean }) {
  const presets:readonly {id:string;label:string;size:string;url:string}[]=isH3?h3BandwidthTestURLPresets:bandwidthTestURLPresets;
  const selected = presets.find((preset) => preset.url === value.trim());
  return <div className="field bandwidth-url-preset">
    <Select fullWidth placeholder="自定义地址" value={selected?.id ?? null} variant="secondary" onChange={(key) => {
      const preset = presets.find((item) => item.id === String(key));
      if (preset) onChange(preset.url);
    }}>
      <Label>{isH3?"H3 测速预设":"常用地址"}</Label>
      <Select.Trigger aria-label={isH3?"H3 测速预设":"常用测速地址"}><Select.Value>{selected?.label ?? "自定义地址"}</Select.Value><Select.Indicator /></Select.Trigger>
      <Select.Popover><Heading className="sr-only" slot="title">常用测速地址</Heading><ListBox aria-label={isH3?"H3 测速预设候选":"常用测速地址候选"} items={presets}>
        {(preset) => <ListBox.Item id={preset.id} textValue={`${preset.label} ${preset.size}`}><span className="bandwidth-url-option"><strong>{preset.label}</strong><small>{preset.size}</small></span><ListBox.ItemIndicator /></ListBox.Item>}
      </ListBox></Select.Popover>
    </Select>
  </div>;
}
