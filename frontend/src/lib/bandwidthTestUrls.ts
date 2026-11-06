export const DEFAULT_BANDWIDTH_TEST_URL = "https://speed.cloudflare.com/__down?bytes=99999999";

export const DEFAULT_H3_BANDWIDTH_TEST_URL = "https://h3.speed.cloudflare.com/__down?bytes=99999999";
export const h3BandwidthTestURLPresets = [{id:"cloudflare-h3",label:"Cloudflare HTTP/3 官方",size:"约 95 MiB",url:DEFAULT_H3_BANDWIDTH_TEST_URL}] as const;

export const bandwidthTestURLPresets = [
  { id: "cloudflare", label: "Cloudflare 官方", size: "约 95 MiB", url: DEFAULT_BANDWIDTH_TEST_URL },
  { id: "parallels", label: "Parallels", size: "约 318 MiB", url: "https://download.parallels.com/desktop/v17/17.1.1-51537/ParallelsDesktop-17.1.1-51537.dmg" },
  { id: "openbsd", label: "OpenBSD 7.9", size: "约 195 MiB", url: "https://cloudflare.cdn.openbsd.org/pub/OpenBSD/7.9/src.tar.gz" },
] as const;
