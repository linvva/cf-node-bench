import type { DownloadCheck, FailureReason } from "../types";

export const failureLabels: Record<FailureReason, string> = {
  invalid_ip: "无效 IP",
  invalid_port: "无效端口",
  invalid_tag: "无效标签",
  port_filtered: "端口排除",
  country_filtered: "地区排除",
  dns: "DNS 解析失败",
  quic: "QUIC 握手失败",
  quic_timeout: "QUIC 握手超时",
  protocol: "协议协商失败",
  tcp: "TCP 连接失败",
  tls: "TLS 握手失败",
  tls_timeout: "TLS 握手超时",
  timeout: "请求超时",
  http_status: "HTTP 状态异常",
  cancelled: "已取消",
  download: "下载失败",
  bandwidth_below_minimum: "带宽未达门槛",
};

export function failureCounts(failures: DownloadCheck["failures"] = {}) {
  return Object.entries(failures)
    .filter(([, count]) => count > 0)
    .sort((left, right) => right[1] - left[1])
    .map(([reason, count]) => `${failureLabels[reason as FailureReason] ?? reason} ${count}`);
}

export function downloadCheckText(check: DownloadCheck) {
  if (!check.sampled) return `${check.host} 暂无可用候选可供抽测`;
  const failures = failureCounts(check.failures);
  return `${check.host} · ${check.passed} / ${check.sampled} 个候选可下载${failures.length ? ` · ${failures.join("、")}` : ""}`;
}
