import { useEffect, useState } from "react";
import { Button, Input, toast } from "@heroui/react";
import { Save } from "lucide-react";
import { actions, useAppStore } from "../../store";
import type { Settings } from "../../types";
import { CountryPicker } from "./CountryPicker";

type IntegerSetting = "tcpConcurrency" | "httpsConcurrency" | "bandwidthConcurrency" |
  "connectTimeoutMs" | "requestTimeoutMs" | "bandwidthTimeoutMs" | "sourceTimeoutMs" |
  "sourceRetries" | "tcpProbeCount" | "httpsProbeCount" | "tcpCandidateCount" |
  "bandwidthCandidates" | "finalResultCount";

const constraints: Record<IntegerSetting, [number, number, string]> = {
  tcpConcurrency: [1, 256, "TCP 并发数"],
  httpsConcurrency: [1, 64, "HTTPS 并发数"],
  bandwidthConcurrency: [1, 10, "带宽并发数"],
  connectTimeoutMs: [100, 30000, "连接超时"],
  requestTimeoutMs: [500, 60000, "HTTPS 超时"],
  bandwidthTimeoutMs: [1000, 120000, "带宽总超时"],
  sourceTimeoutMs: [500, 60000, "数据源超时"],
  sourceRetries: [0, 3, "数据源重试次数"],
  tcpProbeCount: [1, 10, "TCP 探测次数"],
  httpsProbeCount: [1, 10, "HTTPS 探测次数"],
  tcpCandidateCount: [1, 5000, "TCP 候选数"],
  bandwidthCandidates: [1, 500, "带宽候选数"],
  finalResultCount: [1, 100, "最终结果数"],
};

export function SettingsPage() {
  const saved = useAppStore((state) => state.settings);
  const [form, setForm] = useState(saved);
  const [error, setError] = useState("");

  useEffect(() => setForm(saved), [saved]);

  const number = (key: IntegerSetting, value: string) => setForm((current) => ({ ...current, [key]: Number(value) }));
  const save = async () => {
    const message = validateSettings(form);
    setError(message);
    if (message) return;
    await actions.saveSettings(form);
    toast.success("设置已保存");
  };

  return <div className="page" data-testid="settings-page">
    <header className="page-header"><div><h1>测速设置</h1><p>控制探测成本、可用性门槛和候选范围；所有网络操作仍有独立超时。</p></div></header>
    <div className="settings-layout">
      <div className="settings-column">
        <SettingsSection title="并发" description="并发越高不一定越快，可能受本机网络和系统句柄限制。">
          <Field label="TCP 并发" value={form.tcpConcurrency} min={1} max={256} onChange={(value) => number("tcpConcurrency", value)} />
          <Field label="HTTPS 并发" value={form.httpsConcurrency} min={1} max={64} onChange={(value) => number("httpsConcurrency", value)} />
          <Field label="带宽并发" value={form.bandwidthConcurrency} min={1} max={10} onChange={(value) => number("bandwidthConcurrency", value)} />
        </SettingsSection>
        <SettingsSection title="候选池与过滤" description="空白过滤列表表示不限；排除国家的优先级高于允许国家。">
          <Field label="TCP 候选池" value={form.tcpCandidateCount} min={1} max={5000} onChange={(value) => number("tcpCandidateCount", value)} />
          <Field label="带宽候选池" value={form.bandwidthCandidates} min={1} max={500} onChange={(value) => number("bandwidthCandidates", value)} />
          <Field label="最终结果数" value={form.finalResultCount} min={1} max={100} onChange={(value) => number("finalResultCount", value)} />
          <MiBField value={form.maxDownloadBytes} onChange={(value) => setForm((current) => ({ ...current, maxDownloadBytes: value }))} />
          <div className="field field-wide"><label>允许端口</label><Input aria-label="允许端口" value={form.allowedPorts.join(", ")} onChange={(event) => setForm((current) => ({ ...current, allowedPorts: event.target.value.split(/[,\s]+/).filter(Boolean).map(Number) }))} /><small>逗号分隔；留空表示不限</small></div>
          <CountryPicker label="允许国家" value={form.allowedCountries} description="留空表示不限；支持按中文名称或代码搜索" onChange={(value) => setForm((current) => ({ ...current, allowedCountries: value }))} />
          <CountryPicker label="排除国家" value={form.blockedCountries} description="仅匹配数据源提供的国家标签；命中后在 TCP 前排除并显示数量" onChange={(value) => setForm((current) => ({ ...current, blockedCountries: value }))} />
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
        <SettingsSection title="采样与可用性" description="TCP 和 HTTPS 分别通过硬门槛后，才会进入后续阶段。">
          <Field label="TCP 探测次数" value={form.tcpProbeCount} min={1} max={10} onChange={(value) => number("tcpProbeCount", value)} />
          <Field label="HTTPS 探测次数" value={form.httpsProbeCount} min={1} max={10} onChange={(value) => number("httpsProbeCount", value)} />
          <RateField label="TCP 最低成功率" value={form.tcpMinSuccessRate} onChange={(value) => setForm((current) => ({ ...current, tcpMinSuccessRate: value }))} />
          <RateField label="HTTPS 最低成功率" value={form.httpsMinSuccessRate} onChange={(value) => setForm((current) => ({ ...current, httpsMinSuccessRate: value }))} />
        </SettingsSection>
      </div>
    </div>
    <div className="settings-footer">{error && <p className="settings-error">{error}</p>}<Button variant="primary" onPress={() => void save()}><Save size={15} />保存设置</Button></div>
  </div>;
}

export function validateSettings(form: Settings) {
    for (const [key, [min, max, label]] of Object.entries(constraints) as [IntegerSetting, [number, number, string]][]) {
      const value = form[key];
      if (!Number.isInteger(value) || value < min || value > max) return `${label}必须在 ${min} 到 ${max} 之间`;
    }
    if (form.tcpMinSuccessRate < 0.6 || form.tcpMinSuccessRate > 1) return "TCP 最低成功率必须在 60% 到 100% 之间";
    if (form.httpsMinSuccessRate < 0.6 || form.httpsMinSuccessRate > 1) return "HTTPS 最低成功率必须在 60% 到 100% 之间";
    if (form.maxDownloadBytes < 1048576 || form.maxDownloadBytes > 1073741824) return "最大下载量必须在 1 到 1024 MiB 之间";
    if (form.bandwidthCandidates > form.tcpCandidateCount) return "带宽候选数不能大于 TCP 候选数";
    if (form.finalResultCount > form.bandwidthCandidates) return "最终结果数不能大于带宽候选数";
    if (form.allowedPorts.some((port) => !Number.isInteger(port) || port < 1 || port > 65535)) return "端口必须在 1 到 65535 之间";
    if ([...form.allowedCountries, ...form.blockedCountries].some((country) => !/^[A-Z]{2}$/.test(country))) return "国家代码必须是两个大写字母";
    const blocked = new Set(form.blockedCountries);
    const conflict = form.allowedCountries.find((country) => blocked.has(country));
    if (conflict) return `国家 ${conflict} 不能同时出现在允许和排除列表`;
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
