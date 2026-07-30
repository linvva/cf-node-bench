import { useEffect, useMemo, useState } from "react";
import { Button, Checkbox, Input, Switch, Tabs, toast } from "@heroui/react";
import { CheckCircle2, Cloud, GitBranch, MessageCircle, Save, Send, Trash2 } from "lucide-react";
import { actions, useAppStore } from "../../store";
import type { PublicationResult, PublishSaveRequest, PublishSettingsView, PublishTarget } from "../../types";

const labels:Record<PublishTarget,string>={cloudflare:"Cloudflare",github:"GitHub",telegram:"Telegram"};

export function PublishPage(){
  const settings=useAppStore(state=>state.publishSettings);
  const latest=useAppStore(state=>state.history.find(item=>item.state==="completed"));
  const [form,setForm]=useState<PublishSettingsView>(()=>structuredClone(settings));
  const [tokens,setTokens]=useState({cloudflareToken:"",githubToken:"",telegramBotToken:""});
  const [error,setError]=useState("");
  const [busy,setBusy]=useState("");
  useEffect(()=>setForm(structuredClone(settings)),[settings]);

  const request=():PublishSaveRequest=>({settings:form,...tokens});
  const save=async()=>{
    const message=validatePublishSettings(form,tokens);
    if(message){setError(message);return;}
    try{
      setBusy("save");
      await actions.savePublishSettings(request());
      setTokens({cloudflareToken:"",githubToken:"",telegramBotToken:""});
      setError("");
      toast.success("发布设置已保存");
    }catch(reason){setError(String(reason));}finally{setBusy("");}
  };
  const test=async(target:PublishTarget)=>{
    const message=validatePublishSettings(form,tokens,target);
    if(message){setError(message);return;}
    try{setBusy(`test-${target}`);await actions.testPublishTarget(target,request());setError("");toast.success(`${labels[target]} 连接可用`);}catch(reason){setError(String(reason));}finally{setBusy("");}
  };
  const clear=async(target:PublishTarget)=>{
    try{setBusy(`clear-${target}`);await actions.clearPublishCredential(target);setTokens(current=>({...current,[target==="cloudflare"?"cloudflareToken":target==="github"?"githubToken":"telegramBotToken"]:""}));toast.success(`${labels[target]} 凭据已清除`);}catch(reason){setError(String(reason));}finally{setBusy("");}
  };
  const publishLatest=async()=>{
    if(!latest)return;
    try{setBusy("publish");await actions.publishRun(latest.runId,"all");toast.success("发布任务已加入队列");}catch(reason){setError(String(reason));}finally{setBusy("");}
  };

  const patch=<K extends keyof PublishSettingsView>(key:K,value:PublishSettingsView[K])=>setForm(current=>({...current,[key]:value}));
  const statusByTarget=useMemo(()=>Object.fromEntries((latest?.publications||[]).map(value=>[value.target,value])) as Partial<Record<PublishTarget,PublicationResult>>,[latest]);
  const preview=previewLine(form);

  return <div className="page publish-page" data-testid="publish-page">
    <header className="page-header"><div><h1>结果发布</h1><p>测速完成后由后端发布最终结果；发布失败不会改变测速状态。</p></div><div className="page-actions"><Button variant="secondary" isDisabled={!latest||busy==="publish"} onPress={()=>void publishLatest()}><Send size={16}/>发布最近结果</Button><Button variant="primary" isDisabled={busy==="save"} onPress={()=>void save()}><Save size={16}/>保存设置</Button></div></header>

    <div className="publish-overview">
      <section className="panel publish-section"><div className="publish-heading"><div><h2>输出字段</h2><p>TXT、GitHub 与 Telegram 详细列表使用同一行格式。</p></div></div><div className="publish-options">
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
        <CredentialField label="API Token" configured={form.cloudflare.tokenConfigured} value={tokens.cloudflareToken} onChange={cloudflareToken=>setTokens(current=>({...current,cloudflareToken}))} onClear={()=>void clear("cloudflare")} clearing={busy==="clear-cloudflare"}/>
      </div><TargetActions target="cloudflare" busy={busy} onTest={test}/>
    </section>

    <section className="panel publish-section target-section">
      <TargetHeading icon={<GitBranch size={17}/>} title="GitHub Contents" enabled={form.github.enabled} status={statusByTarget.github} onEnabled={enabled=>patch("github",{...form.github,enabled})}/>
      <div className="publish-target-grid">
        <TextField label="Owner" value={form.github.owner} onChange={owner=>patch("github",{...form.github,owner})}/>
        <TextField label="Repository" value={form.github.repository} onChange={repository=>patch("github",{...form.github,repository})}/>
        <TextField label="Branch" value={form.github.branch} onChange={branch=>patch("github",{...form.github,branch})}/>
        <TextField label="文件路径" value={form.github.path} onChange={path=>patch("github",{...form.github,path})}/>
        <CredentialField label="Personal Access Token" configured={form.github.tokenConfigured} value={tokens.githubToken} onChange={githubToken=>setTokens(current=>({...current,githubToken}))} onClear={()=>void clear("github")} clearing={busy==="clear-github"}/>
      </div><TargetActions target="github" busy={busy} onTest={test}/>
    </section>

    <section className="panel publish-section target-section">
      <TargetHeading icon={<MessageCircle size={17}/>} title="Telegram Bot" enabled={form.telegram.enabled} status={statusByTarget.telegram} onEnabled={enabled=>patch("telegram",{...form.telegram,enabled})}/>
      <div className="publish-target-grid">
        <div className="field field-wide"><label>推送内容</label><ModeTabs label="Telegram 推送内容" value={form.telegram.contentMode} options={[{id:"summary",label:"仅汇总"},{id:"details",label:"汇总与节点列表"}]} onChange={contentMode=>patch("telegram",{...form.telegram,contentMode:contentMode as "summary"|"details"})}/><small>汇总包含耗时、通过节点数和 Cloudflare/GitHub 发布状态。</small></div>
        <TextField label="Chat ID" value={form.telegram.chatId} onChange={chatId=>patch("telegram",{...form.telegram,chatId})}/>
        <CredentialField label="Bot Token" configured={form.telegram.tokenConfigured} value={tokens.telegramBotToken} onChange={telegramBotToken=>setTokens(current=>({...current,telegramBotToken}))} onClear={()=>void clear("telegram")} clearing={busy==="clear-telegram"}/>
      </div><TargetActions target="telegram" busy={busy} onTest={test}/>
    </section>
    {error&&<div className="settings-footer"><p className="settings-error">{error}</p></div>}
  </div>;
}

export function validatePublishSettings(form:PublishSettingsView,tokens:{cloudflareToken:string;githubToken:string;telegramBotToken:string},target?:PublishTarget){
  if(!Number.isInteger(form.request.timeoutMs)||form.request.timeoutMs<1000||form.request.timeoutMs>60000)return "发布请求超时必须在 1000 到 60000 ms 之间";
  if(!Number.isInteger(form.request.maxRetries)||form.request.maxRetries<0||form.request.maxRetries>5)return "发布重试次数必须在 0 到 5 之间";
  if(!Number.isInteger(form.request.retryDelayMs)||form.request.retryDelayMs<0||form.request.retryDelayMs>10000)return "发布重试间隔必须在 0 到 10000 ms 之间";
  if(form.cloudflare.ttl!==1&&(!Number.isInteger(form.cloudflare.ttl)||form.cloudflare.ttl<60||form.cloudflare.ttl>86400))return "Cloudflare TTL 必须为 1 或在 60 到 86400 之间";
  const checking=(value:PublishTarget)=>target===value||(!target&&form[value].enabled);
  if(checking("cloudflare")&&(!form.cloudflare.zoneId.trim()||!form.cloudflare.recordName.trim()||(!form.cloudflare.tokenConfigured&&!tokens.cloudflareToken.trim())))return "启用 Cloudflare 前必须填写 Token、Zone ID 和记录名";
  if(!form.github.path.trim()||form.github.path.startsWith("/")||form.github.path.split("/").includes(".."))return "GitHub 文件路径必须是仓库内的有效相对路径";
  if(checking("github")&&(!form.github.owner.trim()||!form.github.repository.trim()||!form.github.branch.trim()||(!form.github.tokenConfigured&&!tokens.githubToken.trim())))return "启用 GitHub 前必须填写 Token、仓库、分支和文件路径";
  if(checking("telegram")&&(!form.telegram.chatId.trim()||(!form.telegram.tokenConfigured&&!tokens.telegramBotToken.trim())))return "启用 Telegram 前必须填写 Bot Token 和 Chat ID";
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
  return <span className={`publication-state ${result.state}`} title={result.message}>{text}{result.items>0?` · ${result.items} 条`:""}{result.message?` · ${result.message}`:""}</span>;
}

function TargetActions({target,busy,onTest}:{target:PublishTarget;busy:string;onTest:(target:PublishTarget)=>Promise<void>}){
  return <div className="target-actions"><Button variant="secondary" isDisabled={busy===`test-${target}`} onPress={()=>void onTest(target)}><CheckCircle2 size={15}/>测试连接</Button></div>;
}

function ModeTabs({label,value,options,onChange}:{label:string;value:string;options:{id:string;label:string}[];onChange:(value:string)=>void}){
  return <Tabs className="mode-tabs" variant="secondary" selectedKey={value} onSelectionChange={key=>onChange(String(key))}><Tabs.ListContainer><Tabs.List aria-label={label}>{options.map(option=><Tabs.Tab id={option.id} key={option.id}>{option.label}</Tabs.Tab>)}</Tabs.List></Tabs.ListContainer>{options.map(option=><Tabs.Panel className="sr-only" id={option.id} key={option.id}>{option.label}</Tabs.Panel>)}</Tabs>;
}

function TextField({label,value,placeholder,onChange}:{label:string;value:string;placeholder?:string;onChange:(value:string)=>void}){
  return <div className="field"><label>{label}</label><Input aria-label={label} value={value} placeholder={placeholder} onChange={event=>onChange(event.target.value)}/></div>;
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
