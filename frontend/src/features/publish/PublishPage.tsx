import { useEffect, useMemo, useState } from "react";
import { Button, Checkbox, Input, Modal, Switch, Tabs, toast } from "@heroui/react";
import { CheckCircle2, Cloud, Code2, Copy, ExternalLink, FileText, GitBranch, MessageCircle, Save, Send, Trash2, X } from "lucide-react";
import { actions, useAppStore } from "../../store";
import type { PublicationResult, PublishCredentialTarget, PublishSaveRequest, PublishSettingsView, PublishTarget } from "../../types";
import workerSource from "../../../../deploy/telegram-relay/worker.js?raw";

const labels:Record<PublishTarget,string>={cloudflare:"Cloudflare",github:"GitHub 仓库",gist:"GitHub Gist",telegram:"Telegram"};
type PublishTokens={cloudflareToken:string;githubToken:string;gistToken:string;telegramBotToken:string;telegramRelayKey:string};
type SetupGuideTarget="cloudflare"|"github"|"gist"|"";
const emptyTokens:PublishTokens={cloudflareToken:"",githubToken:"",gistToken:"",telegramBotToken:"",telegramRelayKey:""};

export function PublishPage(){
  const settings=useAppStore(state=>state.publishSettings);
  const latest=useAppStore(state=>state.history.find(item=>item.state==="completed"));
  const [form,setForm]=useState<PublishSettingsView>(()=>structuredClone(settings));
  const [tokens,setTokens]=useState<PublishTokens>(emptyTokens);
  const [error,setError]=useState("");
  const [busy,setBusy]=useState("");
  const [setupGuide,setSetupGuide]=useState<SetupGuideTarget>("");
  const [relayGuideOpen,setRelayGuideOpen]=useState(false);
  useEffect(()=>setForm(structuredClone(settings)),[settings]);

  const request=():PublishSaveRequest=>({settings:form,...tokens});
  const save=async()=>{
    const message=validatePublishSettings(form,tokens);
    if(message){setError(message);return;}
    try{
      setBusy("save");
      await actions.savePublishSettings(request());
      setTokens(emptyTokens);
      setError("");
      toast.success("发布设置已保存");
    }catch(reason){setError(String(reason));}finally{setBusy("");}
  };
  const test=async(target:PublishTarget)=>{
    const message=validatePublishSettings(form,tokens,target);
    if(message){setError(message);return;}
    try{setBusy(`test-${target}`);await actions.testPublishTarget(target,request());setError("");toast.success(target==="gist"?"GitHub Gist 可访问；写权限将在发布时验证":`${labels[target]} 连接可用`);}catch(reason){setError(String(reason));}finally{setBusy("");}
  };
  const clear=async(target:PublishCredentialTarget,key:keyof PublishTokens,label:string)=>{
    try{setBusy(`clear-${target}`);await actions.clearPublishCredential(target);setTokens(current=>({...current,[key]:""}));toast.success(`${label}已清除`);}catch(reason){setError(String(reason));}finally{setBusy("");}
  };
  const publishLatest=async()=>{
    if(!latest)return;
    try{setBusy("publish");await actions.publishRun(latest.runId,"all");toast.success("发布任务已加入队列");}catch(reason){setError(String(reason));}finally{setBusy("");}
  };

  const patch=<K extends keyof PublishSettingsView>(key:K,value:PublishSettingsView[K])=>setForm(current=>({...current,[key]:value}));
  const statusByTarget=useMemo(()=>Object.fromEntries((latest?.publications||[]).map(value=>[value.target,value])) as Partial<Record<PublishTarget,PublicationResult>>,[latest]);
  const enabledTargets=useMemo(()=>(Object.keys(labels) as PublishTarget[]).filter(target=>settings[target].enabled).map(target=>labels[target]),[settings]);
  const preview=previewLine(form);

  return <div className="page publish-page" data-testid="publish-page">
    <header className="page-header"><div><h1>结果发布</h1><p>成功完成的测速会自动发布到已启用目标；取消测速不会发布。</p></div><div className="page-action-area"><div className="page-actions"><Button variant="secondary" isDisabled={!latest||busy==="publish"} onPress={()=>void publishLatest()}><Send size={16}/>发布最近结果</Button><Button variant="primary" isDisabled={busy==="save"} onPress={()=>void save()}><Save size={16}/>保存设置</Button></div><p className="page-action-status" role={error?"alert":undefined} title={error}>{error||"\u00a0"}</p></div></header>

    <div className={`publish-auto-state ${enabledTargets.length?"active":"inactive"}`} aria-live="polite"><CheckCircle2 size={16}/><div><strong>{enabledTargets.length?"自动发布已生效":"自动发布尚未生效"}</strong><span>{enabledTargets.length?`测速成功完成后将自动发布到：${enabledTargets.join("、")}。发布失败不会改变测速结果。`:"当前没有已启用的发布目标；启用目标并保存设置后，测速成功完成时会自动发布。"}</span></div></div>

    <div className="publish-overview">
      <section className="panel publish-section"><div className="publish-heading"><div><h2>输出字段</h2><p>TXT、GitHub 仓库、Gist 与 Telegram 详细列表使用同一行格式。</p></div></div><div className="publish-options">
        <Check label="国家代码" selected={form.output.country} onChange={country=>patch("output",{...form.output,country})}/>
        <Check label="TCP P95" selected={form.output.tcpP95} onChange={tcpP95=>patch("output",{...form.output,tcpP95})}/>
        <Check label="HTTP 平均延迟" selected={form.output.httpLatency} onChange={httpLatency=>patch("output",{...form.output,httpLatency})}/>
        <Check label="下载带宽" selected={form.output.bandwidth} onChange={bandwidth=>patch("output",{...form.output,bandwidth})}/>
      </div><code className="publish-preview">{preview}</code></section>
      <section className="panel publish-section"><div className="publish-heading"><div><h2>请求策略</h2><p>仅网络错误、408、429 和 5xx 会重试。</p></div></div><div className="field-grid">
        <NumberField label="请求超时 (ms)" value={form.request.timeoutMs} min={1000} max={60000} onChange={timeoutMs=>patch("request",{...form.request,timeoutMs})}/>
        <NumberField label="额外重试次数" value={form.request.maxRetries} min={0} max={5} onChange={maxRetries=>patch("request",{...form.request,maxRetries})}/>
        <NumberField label="重试间隔 (ms)" value={form.request.retryDelayMs} min={0} max={10000} onChange={retryDelayMs=>patch("request",{...form.request,retryDelayMs})}/>
      </div></section>
    </div>

    <section className="panel publish-section target-section">
      <TargetHeading icon={<Cloud size={17}/>} title="Cloudflare DNS" enabled={form.cloudflare.enabled} status={statusByTarget.cloudflare} onEnabled={enabled=>patch("cloudflare",{...form.cloudflare,enabled})}/>
      <div className="publish-target-grid">
        <div className="field field-wide"><label>记录类型</label><ModeTabs label="Cloudflare 记录类型" value={form.cloudflare.recordType} options={[{id:"A",label:"A 记录"},{id:"TXT",label:"TXT 记录"}]} onChange={recordType=>patch("cloudflare",{...form.cloudflare,recordType:recordType as "A"|"TXT"})}/><small>{form.cloudflare.recordType==="A"?"仅发布端口为 443 的 IPv4，内容为纯 IP。":"发布全部最终节点；Proxied 设置不会应用。"}</small></div>
        <TextField label="Zone ID" value={form.cloudflare.zoneId} onChange={zoneId=>patch("cloudflare",{...form.cloudflare,zoneId})}/>
        <TextField label="记录名" value={form.cloudflare.recordName} placeholder="cf.example.com" onChange={recordName=>patch("cloudflare",{...form.cloudflare,recordName})}/>
        <NumberField label="TTL" value={form.cloudflare.ttl} min={1} max={86400} onChange={ttl=>patch("cloudflare",{...form.cloudflare,ttl})}/>
        {form.cloudflare.recordType==="A"&&<div className="switch-field"><Switch isSelected={form.cloudflare.proxied} onChange={proxied=>patch("cloudflare",{...form.cloudflare,proxied})}><Switch.Content><Switch.Control><Switch.Thumb/></Switch.Control><span>启用 Cloudflare 代理</span></Switch.Content></Switch><small>关闭时 DNS 返回节点原始 IP。</small></div>}
        <CredentialField label="API Token" configured={form.cloudflare.tokenConfigured} value={tokens.cloudflareToken} onChange={cloudflareToken=>setTokens(current=>({...current,cloudflareToken}))} onClear={()=>void clear("cloudflare","cloudflareToken","Cloudflare 凭据")} clearing={busy==="clear-cloudflare"}/>
        <div className="field-wide target-guide"><small>需要目标域名的 Zone ID，以及仅限该 Zone 的 DNS 编辑 Token；记录无需预先创建。</small><Button variant="secondary" aria-label="Cloudflare 配置引导" onPress={()=>setSetupGuide("cloudflare")}><Cloud size={15}/>配置引导</Button></div>
      </div><TargetActions target="cloudflare" busy={busy} onTest={test}/>
    </section>

    <section className="panel publish-section target-section">
      <TargetHeading icon={<GitBranch size={17}/>} title="GitHub 仓库" enabled={form.github.enabled} status={statusByTarget.github} onEnabled={enabled=>patch("github",{...form.github,enabled})}/>
      <div className="publish-target-grid">
        <TextField label="Owner" value={form.github.owner} onChange={owner=>patch("github",{...form.github,owner})}/>
        <TextField label="Repository" value={form.github.repository} onChange={repository=>patch("github",{...form.github,repository})}/>
        <TextField label="Branch" value={form.github.branch} onChange={branch=>patch("github",{...form.github,branch})}/>
        <TextField label="文件路径" value={form.github.path} onChange={path=>patch("github",{...form.github,path})}/>
        <CredentialField label="Personal Access Token" configured={form.github.tokenConfigured} value={tokens.githubToken} onChange={githubToken=>setTokens(current=>({...current,githubToken}))} onClear={()=>void clear("github","githubToken","GitHub 仓库凭据")} clearing={busy==="clear-github"}/>
        <div className="field-wide target-guide"><small>目标分支必须已存在；结果文件可由应用创建，Token 只需目标仓库的 Contents 写权限。</small><Button variant="secondary" aria-label="GitHub 仓库配置引导" onPress={()=>setSetupGuide("github")}><GitBranch size={15}/>配置引导</Button></div>
      </div><TargetActions target="github" busy={busy} onTest={test}/>
    </section>

    <section className="panel publish-section target-section">
      <TargetHeading icon={<FileText size={17}/>} title="GitHub Gist" enabled={form.gist.enabled} status={statusByTarget.gist} onEnabled={enabled=>patch("gist",{...form.gist,enabled})}/>
      <div className="publish-target-grid">
        <TextField label="Gist ID" value={form.gist.gistId} placeholder="例如 6f4a..." help="仅更新已有 Gist；Secret Gist 只是未公开列出，任何获得链接的人仍可访问。" onChange={gistId=>patch("gist",{...form.gist,gistId})}/>
        <TextField label="文件名" value={form.gist.filename} help="同名文件会被更新，Gist 中的其他文件不受影响。" onChange={filename=>patch("gist",{...form.gist,filename})}/>
        <CredentialField label="Gist Personal Access Token" configured={form.gist.tokenConfigured} value={tokens.gistToken} onChange={gistToken=>setTokens(current=>({...current,gistToken}))} onClear={()=>void clear("gist","gistToken","GitHub Gist 凭据")} clearing={busy==="clear-gist"}/>
        <div className="field-wide target-guide"><small>首次配置需要先创建包含目标文件的 Gist，再生成仅有 Gists 写权限的独立 Token。</small><Button variant="secondary" aria-label="Gist 配置引导" onPress={()=>setSetupGuide("gist")}><FileText size={15}/>配置引导</Button></div>
      </div><TargetActions target="gist" busy={busy} onTest={test}/>
    </section>

    <section className="panel publish-section target-section">
      <TargetHeading icon={<MessageCircle size={17}/>} title="Telegram Bot" enabled={form.telegram.enabled} status={statusByTarget.telegram} onEnabled={enabled=>patch("telegram",{...form.telegram,enabled})}/>
      <div className="publish-target-grid">
        <div className="field field-wide"><label>投递方式</label><ModeTabs label="Telegram 投递方式" value={form.telegram.deliveryMode} options={[{id:"direct",label:"直连 Telegram"},{id:"relay",label:"专属中继"}]} onChange={deliveryMode=>patch("telegram",{...form.telegram,deliveryMode:deliveryMode as "direct"|"relay"})}/><small>{form.telegram.deliveryMode==="direct"?"应用直接连接 Telegram Bot API，不使用系统代理。":"应用通过 HTTPS 连接你自己部署的 Worker，再由 Worker 转发到 Telegram。"}</small></div>
        <div className="field field-wide"><label>推送内容</label><ModeTabs label="Telegram 推送内容" value={form.telegram.contentMode} options={[{id:"summary",label:"仅汇总"},{id:"details",label:"汇总与节点列表"}]} onChange={contentMode=>patch("telegram",{...form.telegram,contentMode:contentMode as "summary"|"details"})}/><small>汇总包含耗时、通过节点数及 Cloudflare、GitHub 仓库和 Gist 发布状态。</small></div>
        <TextField label="Chat ID" value={form.telegram.chatId} onChange={chatId=>patch("telegram",{...form.telegram,chatId})}/>
        <CredentialField label="Bot Token" configured={form.telegram.tokenConfigured} value={tokens.telegramBotToken} onChange={telegramBotToken=>setTokens(current=>({...current,telegramBotToken}))} onClear={()=>void clear("telegram","telegramBotToken","Telegram Bot Token ")} clearing={busy==="clear-telegram"}/>
        {form.telegram.deliveryMode==="relay"&&<>
          <TextField label="中继 URL" value={form.telegram.relayUrl} placeholder="https://relay.example.workers.dev/telegram" help="填写专属 Worker 的完整 /telegram 地址。" onChange={relayUrl=>patch("telegram",{...form.telegram,relayUrl})}/>
          <CredentialField label="中继访问密钥" configured={form.telegram.relayKeyConfigured} value={tokens.telegramRelayKey} onChange={telegramRelayKey=>setTokens(current=>({...current,telegramRelayKey}))} onClear={()=>void clear("telegramRelay","telegramRelayKey","Telegram 中继访问密钥 ")} clearing={busy==="clear-telegramRelay"}/>
          <div className="field-wide relay-warning"><small>只使用部署在自己账户下的专属中继。Bot Token、Chat ID 和消息会经该 Worker 转发，使用第三方公共中继会造成凭据与内容泄露。</small><Button variant="secondary" onPress={()=>setRelayGuideOpen(true)}><Code2 size={15}/>部署 Worker</Button></div>
        </>}
      </div><TargetActions target="telegram" busy={busy} onTest={test}/>
    </section>
    {setupGuide==="cloudflare"&&<CloudflareGuide onClose={()=>setSetupGuide("")}/>}
    {setupGuide==="github"&&<GitHubGuide onClose={()=>setSetupGuide("")}/>}
    {setupGuide==="gist"&&<GistGuide filename={form.gist.filename.trim()||"ip.txt"} onClose={()=>setSetupGuide("")}/>}
    {relayGuideOpen&&<RelayGuide onClose={()=>setRelayGuideOpen(false)}/>}
  </div>;
}

export function validatePublishSettings(form:PublishSettingsView,tokens:PublishTokens,target?:PublishTarget){
  if(!Number.isInteger(form.request.timeoutMs)||form.request.timeoutMs<1000||form.request.timeoutMs>60000)return "发布请求超时必须在 1000 到 60000 ms 之间";
  if(!Number.isInteger(form.request.maxRetries)||form.request.maxRetries<0||form.request.maxRetries>5)return "发布重试次数必须在 0 到 5 之间";
  if(!Number.isInteger(form.request.retryDelayMs)||form.request.retryDelayMs<0||form.request.retryDelayMs>10000)return "发布重试间隔必须在 0 到 10000 ms 之间";
  if(form.cloudflare.ttl!==1&&(!Number.isInteger(form.cloudflare.ttl)||form.cloudflare.ttl<60||form.cloudflare.ttl>86400))return "Cloudflare TTL 必须为 1 或在 60 到 86400 之间";
  const checking=(value:PublishTarget)=>target===value||(!target&&form[value].enabled);
  if(checking("cloudflare")&&(!form.cloudflare.zoneId.trim()||!form.cloudflare.recordName.trim()||(!form.cloudflare.tokenConfigured&&!tokens.cloudflareToken.trim())))return "启用 Cloudflare 前必须填写 Token、Zone ID 和记录名";
  if(!form.github.path.trim()||form.github.path.startsWith("/")||form.github.path.split("/").includes(".."))return "GitHub 文件路径必须是仓库内的有效相对路径";
  if(checking("github")&&(!form.github.owner.trim()||!form.github.repository.trim()||!form.github.branch.trim()||(!form.github.tokenConfigured&&!tokens.githubToken.trim())))return "启用 GitHub 前必须填写 Token、仓库、分支和文件路径";
  if(!form.gist.filename.trim())return "Gist 文件名不能为空";
  if(checking("gist")&&(!form.gist.gistId.trim()||(!form.gist.tokenConfigured&&!tokens.gistToken.trim())))return "启用 Gist 前必须填写 Token、Gist ID 和文件名";
  if(form.telegram.relayUrl.trim()){
    try{const relay=new URL(form.telegram.relayUrl);if(relay.protocol!=="https:"||!relay.host||relay.pathname!=="/telegram"||relay.username||relay.password||relay.search||relay.hash)return "Telegram 中继 URL 必须是以 /telegram 结尾且不含凭据和查询参数的 HTTPS 地址";}catch{return "Telegram 中继 URL 必须是以 /telegram 结尾且不含凭据和查询参数的 HTTPS 地址";}
  }
  if(checking("telegram")&&(!form.telegram.chatId.trim()||(!form.telegram.tokenConfigured&&!tokens.telegramBotToken.trim())))return "启用 Telegram 前必须填写 Bot Token 和 Chat ID";
  if(checking("telegram")&&form.telegram.deliveryMode==="relay"&&(!form.telegram.relayUrl.trim()||(!form.telegram.relayKeyConfigured&&!tokens.telegramRelayKey.trim())))return "启用 Telegram 专属中继前必须填写中继 URL 和访问密钥";
  return "";
}

function previewLine(form:PublishSettingsView){
  const values=[`104.18.1.20:443${form.output.country?"#US":""}`];
  if(form.output.tcpP95)values.push("TCP22ms");
  if(form.output.httpLatency)values.push("HTTP44ms");
  if(form.output.bandwidth)values.push("186Mbps");
  return values.join("|");
}

function TargetHeading({icon,title,enabled,status,onEnabled}:{icon:React.ReactNode;title:string;enabled:boolean;status?:PublicationResult;onEnabled:(value:boolean)=>void}){
  return <div className="publish-heading"><div className="target-title"><span>{icon}</span><div><h2>{title}</h2><p>{status?<PublicationStatus result={status}/>:"尚无发布记录"}</p></div></div><Switch aria-label={`启用 ${title}`} isSelected={enabled} onChange={onEnabled}><Switch.Content><Switch.Control><Switch.Thumb/></Switch.Control></Switch.Content></Switch></div>;
}

export function PublicationStatus({result}:{result:PublicationResult}){
  const text={queued:"等待发布",running:"正在发布",succeeded:"发布成功",failed:"发布失败",skipped:"已跳过"}[result.state];
  const description=`${text}${result.items>0?` · ${result.items} 条`:""}${result.message?` · ${result.message}`:""}`;
  const copyRawURL=async()=>{
    if(!result.url)return;
    try{await navigator.clipboard.writeText(latestGistRawURL(result.url));toast.success("已复制固定 Raw 地址；它始终指向 Gist 最新内容，更新后可能有短暂缓存");}
    catch(reason){toast.danger(`复制失败：${String(reason)}`);}
  };
  return <span className={`publication-state ${result.state}`}><span className="publication-state-text" title={result.message}>{description}</span>{result.url&&<><a className="publication-raw-link" href={result.url} target="_blank" rel="noreferrer" aria-label="打开 Gist Raw 文件" onClick={event=>{if(window.runtime?.BrowserOpenURL){event.preventDefault();window.runtime.BrowserOpenURL(result.url!);}}}><ExternalLink size={11}/>Raw</a><span title="复制固定 Raw 地址"><Button className="publication-raw-copy" isIconOnly variant="tertiary" aria-label="复制固定 Gist Raw 地址" onPress={()=>void copyRawURL()}><Copy size={11}/></Button></span></>}</span>;
}

export function latestGistRawURL(rawURL:string){
  try{
    const value=new URL(rawURL);
    if(value.hostname!=="gist.githubusercontent.com")return rawURL;
    const parts=value.pathname.split("/").filter(Boolean); const rawIndex=parts.indexOf("raw");
    if(rawIndex<0||parts.length<=rawIndex+2||!/^[0-9a-f]{7,64}$/i.test(parts[rawIndex+1]))return rawURL;
    parts.splice(rawIndex+1,1); value.pathname=`/${parts.join("/")}`; return value.toString();
  }catch{return rawURL;}
}

function TargetActions({target,busy,onTest}:{target:PublishTarget;busy:string;onTest:(target:PublishTarget)=>Promise<void>}){
  return <div className="target-actions"><Button variant="secondary" isDisabled={busy===`test-${target}`} onPress={()=>void onTest(target)}><CheckCircle2 size={15}/>测试连接</Button></div>;
}

function ModeTabs({label,value,options,onChange}:{label:string;value:string;options:{id:string;label:string}[];onChange:(value:string)=>void}){
  return <Tabs className="mode-tabs" variant="secondary" selectedKey={value} onSelectionChange={key=>onChange(String(key))}><Tabs.ListContainer><Tabs.List aria-label={label}>{options.map(option=><Tabs.Tab id={option.id} key={option.id}>{option.label}</Tabs.Tab>)}</Tabs.List></Tabs.ListContainer>{options.map(option=><Tabs.Panel className="sr-only" id={option.id} key={option.id}>{option.label}</Tabs.Panel>)}</Tabs>;
}

function TextField({label,value,placeholder,help,onChange}:{label:string;value:string;placeholder?:string;help?:string;onChange:(value:string)=>void}){
  return <div className="field"><label>{label}</label><Input aria-label={label} value={value} placeholder={placeholder} onChange={event=>onChange(event.target.value)}/>{help&&<small>{help}</small>}</div>;
}

function NumberField({label,value,min,max,onChange}:{label:string;value:number;min:number;max:number;onChange:(value:number)=>void}){
  return <div className="field"><label>{label}</label><Input type="number" aria-label={label} value={String(value)} min={min} max={max} onChange={event=>onChange(Number(event.target.value))}/><small>{min} – {max}</small></div>;
}

function CredentialField({label,configured,value,onChange,onClear,clearing}:{label:string;configured:boolean;value:string;onChange:(value:string)=>void;onClear:()=>void;clearing:boolean}){
  return <div className="credential-field field-wide"><div className="field"><label>{label}</label><Input type="password" aria-label={label} value={value} placeholder={configured?"已配置；留空则保留":"输入凭据"} onChange={event=>onChange(event.target.value)}/></div><span title="清除凭据"><Button isIconOnly variant="tertiary" aria-label={`清除 ${label}`} isDisabled={!configured||clearing} onPress={onClear}><Trash2 size={15}/></Button></span></div>;
}

function Check({label,selected,onChange}:{label:string;selected:boolean;onChange:(value:boolean)=>void}){
  return <Checkbox isSelected={selected} onChange={onChange}><Checkbox.Content><Checkbox.Control><Checkbox.Indicator/></Checkbox.Control>{label}</Checkbox.Content></Checkbox>;
}

function openExternal(url:string){
  if(window.runtime?.BrowserOpenURL)window.runtime.BrowserOpenURL(url);
  else window.open(url,"_blank","noopener,noreferrer");
}

function CloudflareGuide({onClose}:{onClose:()=>void}){
  return <Modal isOpen onOpenChange={open=>{if(!open)onClose()}}><Modal.Trigger className="sr-only" aria-label="Cloudflare 配置引导弹窗"><span/></Modal.Trigger><Modal.Backdrop variant="opaque"><Modal.Container size="lg" scroll="inside"><Modal.Dialog className="setup-guide"><Modal.Header><Modal.Heading>配置 Cloudflare DNS</Modal.Heading><Modal.CloseTrigger aria-label="关闭 Cloudflare 配置引导"><X size={17}/></Modal.CloseTrigger></Modal.Header><Modal.Body>
    <p className="relay-guide-intro">应用会创建和更新测速结果对应的 DNS 记录，无需提前手动创建记录。</p>
    <ol className="relay-guide-steps">
      <li><strong>复制 Zone ID</strong><span>在 Cloudflare 控制台打开目标域名，从 Overview 页的 API 区域复制 <code>Zone ID</code>。</span></li>
      <li><strong>创建 API Token</strong><span>使用 <code>Edit zone DNS</code> 模板，将资源范围限制为目标 Zone。该权限包含 DNS 记录的读取和编辑。</span></li>
      <li><strong>选择记录模式</strong><span>填写完整记录名。A 模式只发布 443 端口 IPv4，Proxied 仅在此模式生效；TXT 模式发布完整节点行。</span></li>
      <li><strong>保存并测试</strong><span>保存后测试连接。测速成功完成时，应用会自动发布到当前配置的记录名。</span></li>
    </ol>
    <p className="relay-guide-caution">应用只删除 comment 为 <code>Managed by CF Node Bench</code> 的同名 A/TXT 记录，不会删除没有该标记的用户记录。</p>
  </Modal.Body><Modal.Footer><Button variant="secondary" onPress={()=>openExternal("https://dash.cloudflare.com/")}><ExternalLink size={15}/>打开控制台</Button><Button variant="primary" onPress={()=>openExternal("https://dash.cloudflare.com/profile/api-tokens")}><ExternalLink size={15}/>创建 API Token</Button></Modal.Footer></Modal.Dialog></Modal.Container></Modal.Backdrop></Modal>;
}

function GitHubGuide({onClose}:{onClose:()=>void}){
  return <Modal isOpen onOpenChange={open=>{if(!open)onClose()}}><Modal.Trigger className="sr-only" aria-label="GitHub 仓库配置引导弹窗"><span/></Modal.Trigger><Modal.Backdrop variant="opaque"><Modal.Container size="lg" scroll="inside"><Modal.Dialog className="setup-guide"><Modal.Header><Modal.Heading>配置 GitHub 仓库</Modal.Heading><Modal.CloseTrigger aria-label="关闭 GitHub 仓库配置引导"><X size={17}/></Modal.CloseTrigger></Modal.Header><Modal.Body>
    <p className="relay-guide-intro">应用通过 GitHub Contents API 创建或更新结果文件；仓库和目标分支需要预先存在，文件无需预建。</p>
    <ol className="relay-guide-steps">
      <li><strong>准备仓库</strong><span>创建或选择一个仓库，确认准备写入的目标分支已经存在。</span></li>
      <li><strong>填写目标位置</strong><span><code>Owner</code> 是用户或组织名；Repository 不含 <code>.git</code>；文件路径使用仓库内相对路径，例如 <code>ip.txt</code>。</span></li>
      <li><strong>创建 Token</strong><span>新建 Fine-grained personal access token，只选择目标仓库，并将 Repository permissions 中的 <code>Contents</code> 设为 <code>Read and write</code>。</span></li>
      <li><strong>保存并测试</strong><span>测试连接只验证仓库可访问；文件写权限会在首次实际发布时验证。</span></li>
    </ol>
    <p className="relay-guide-caution">请使用独立且有有效期的 Token。若文件路径位于 <code>.github/workflows</code>，GitHub 还会要求额外的 Workflows 写权限。</p>
  </Modal.Body><Modal.Footer><Button variant="secondary" onPress={()=>openExternal("https://github.com/new")}><ExternalLink size={15}/>创建仓库</Button><Button variant="primary" onPress={()=>openExternal("https://github.com/settings/personal-access-tokens/new")}><ExternalLink size={15}/>创建 Token</Button></Modal.Footer></Modal.Dialog></Modal.Container></Modal.Backdrop></Modal>;
}

function GistGuide({filename,onClose}:{filename:string;onClose:()=>void}){
  return <Modal isOpen onOpenChange={open=>{if(!open)onClose()}}><Modal.Trigger className="sr-only" aria-label="GitHub Gist 配置引导"><span/></Modal.Trigger><Modal.Backdrop variant="opaque"><Modal.Container size="lg" scroll="inside"><Modal.Dialog className="setup-guide"><Modal.Header><Modal.Heading>配置 GitHub Gist</Modal.Heading><Modal.CloseTrigger aria-label="关闭 Gist 配置引导"><X size={17}/></Modal.CloseTrigger></Modal.Header><Modal.Body>
    <p className="relay-guide-intro">应用只更新你指定的现有 Gist，不会创建 Gist 或改变其可见性。按以下步骤完成首次配置。</p>
    <ol className="relay-guide-steps">
      <li><strong>创建文件</strong><span>打开 Gist，新建文件 <code>{filename}</code>，填入任意初始内容，然后创建 Secret 或 Public Gist。</span></li>
      <li><strong>填写 Gist ID</strong><span>创建后，从地址 <code>gist.github.com/用户名/GistID</code> 复制最后一段 Gist ID，填回当前页面。</span></li>
      <li><strong>创建 Token</strong><span>新建 Fine-grained personal access token，在 Account permissions 中仅将 <code>Gists</code> 设为 <code>Read and write</code>，并设置合理有效期。</span></li>
      <li><strong>保存并测试</strong><span>Token 只会保存在本机。填写后保存设置并测试连接；首次实际发布会验证写权限。</span></li>
    </ol>
    <p className="relay-guide-caution">Secret Gist 只是不会公开列出，并非私有存储。不要在文件内容中放入 Token、账号凭据或其他敏感信息。</p>
  </Modal.Body><Modal.Footer><Button variant="secondary" onPress={()=>openExternal("https://gist.github.com/")}><ExternalLink size={15}/>创建 Gist</Button><Button variant="primary" onPress={()=>openExternal("https://github.com/settings/personal-access-tokens/new")}><ExternalLink size={15}/>创建 Token</Button></Modal.Footer></Modal.Dialog></Modal.Container></Modal.Backdrop></Modal>;
}

function RelayGuide({onClose}:{onClose:()=>void}){
  const copyWorker=async()=>{try{await navigator.clipboard.writeText(workerSource);toast.success("worker.js 已复制");}catch(reason){toast.danger(`复制失败：${String(reason)}`);}};
  return <Modal isOpen onOpenChange={open=>{if(!open)onClose()}}><Modal.Trigger className="sr-only" aria-label="Worker 部署引导"><span/></Modal.Trigger><Modal.Backdrop variant="opaque"><Modal.Container size="lg" scroll="inside"><Modal.Dialog className="relay-guide"><Modal.Header><Modal.Heading>部署 Telegram 专属中继</Modal.Heading><Modal.CloseTrigger aria-label="关闭部署引导"><X size={17}/></Modal.CloseTrigger></Modal.Header><Modal.Body>
    <p className="relay-guide-intro">将下方 Worker 部署到你自己的 Cloudflare 账户。Worker 只转发本应用需要的两种 Telegram 请求，不保存 Bot Token、Chat ID 或消息。</p>
    <ol className="relay-guide-steps">
      <li><strong>创建 Worker</strong><span>进入 Cloudflare Workers & Pages，新建 Worker，并用下方代码完整替换默认内容。</span></li>
      <li><strong>配置访问密钥</strong><span>在 Worker 的 Variables and Secrets 中添加加密 Secret <code>RELAY_KEY</code>。使用独立的高强度随机值，不要填写 Bot Token。</span></li>
      <li><strong>部署并回填</strong><span>部署后，将 <code>https://你的域名/telegram</code> 和同一个 <code>RELAY_KEY</code> 填入当前页面，再测试连接。</span></li>
    </ol>
    <div className="relay-code-toolbar"><div><strong>worker.js</strong><span>内置版本，可直接部署</span></div><Button variant="secondary" onPress={()=>void copyWorker()}><Copy size={15}/>复制代码</Button></div>
    <pre className="relay-worker-code" aria-label="worker.js 代码"><code>{workerSource.trim()}</code></pre>
    <p className="relay-guide-caution">不要使用第三方提供的公共中继，也不要为 Worker 接入会记录请求正文的日志或调试服务。</p>
  </Modal.Body><Modal.Footer><Button variant="secondary" onPress={()=>openExternal("https://dash.cloudflare.com/")}><ExternalLink size={15}/>打开 Cloudflare 控制台</Button><Button variant="primary" onPress={onClose}>完成</Button></Modal.Footer></Modal.Dialog></Modal.Container></Modal.Backdrop></Modal>;
}
