import { useEffect, useState } from "react";
import { Button, toast } from "@heroui/react";
import { Activity, Database, Moon, Send, Settings, Sun, SunMoon, Upload, Zap } from "lucide-react";
import { actions, useAppStore } from "./store";
import { bridge } from "./lib/bridge";
import { RunWorkspace } from "./features/run/RunWorkspace";
import { SourcesPage } from "./features/sources/SourcesPage";
import { SettingsPage } from "./features/settings/SettingsPage";
import { PublishPage } from "./features/publish/PublishPage";

type Page="run"|"sources"|"publish"|"settings";
type Theme="system"|"light"|"dark";

export function App(){
  const ready=useAppStore(state=>state.ready);
  const migrationNotice=useAppStore(state=>state.migrationNotice);
  const running=useAppStore(state=>state.running);
  const preparingLocal=useAppStore(state=>state.preparingLocal);
  const savingMode=useAppStore(state=>state.savingMode);
  const [dragging,setDragging]=useState(false);
  const [page,setPage]=useState<Page>("run");
  const [theme,setTheme]=useState<Theme>(()=>(localStorage.getItem("theme") as Theme)||"system");
  useEffect(()=>{void actions.init();},[]);
  useEffect(()=>{
    const media=matchMedia("(prefers-color-scheme: dark)");
    const apply=()=>document.documentElement.classList.toggle("dark",theme==="dark"||(theme==="system"&&media.matches));
    apply(); media.addEventListener("change",apply); localStorage.setItem("theme",theme); return()=>media.removeEventListener("change",apply);
  },[theme]);
  useEffect(()=>{
    if(!ready)return;
    let depth=0;
    const isFile=(event:DragEvent)=>Array.from(event.dataTransfer?.types??[]).includes("Files");
    const enter=(event:DragEvent)=>{if(isFile(event)){event.preventDefault();depth++;setDragging(true);}};
    const over=(event:DragEvent)=>{if(isFile(event))event.preventDefault();};
    const leave=()=>{depth=Math.max(0,depth-1);if(!depth)setDragging(false);};
    const reset=()=>{depth=0;setDragging(false);};
    const drop=(event:DragEvent)=>{if(isFile(event)){event.preventDefault();if(!window.runtime?.OnFileDrop)toast.danger("浏览器预览不支持读取本地文件，请使用桌面应用");}reset();};
    const off=bridge.onFileDrop(paths=>{reset();void actions.startLocal(paths).then(id=>{if(id)setPage("run");}).catch(reason=>toast.danger(`启动失败：${String(reason)}`));});
    window.addEventListener("dragenter",enter);window.addEventListener("dragover",over);window.addEventListener("dragleave",leave);window.addEventListener("drop",drop);window.addEventListener("dragend",reset);
    return ()=>{off();window.removeEventListener("dragenter",enter);window.removeEventListener("dragover",over);window.removeEventListener("dragleave",leave);window.removeEventListener("drop",drop);window.removeEventListener("dragend",reset);};
  },[ready]);
  const themeIcon=theme==="system"?<SunMoon size={17}/>:theme==="dark"?<Moon size={17}/>:<Sun size={17}/>;
  const nextTheme=()=>setTheme(value=>value==="system"?"light":value==="light"?"dark":"system");

  if(!ready) return <div className="loading-shell"><Zap size={24}/><span>正在载入工作台</span></div>;
  return <div className="app-shell">
    {dragging&&<div className="file-drop-overlay" role="status"><div><Upload size={36}/><strong>{running||preparingLocal||savingMode?"测速正在运行或准备中，暂不能添加文件":"释放开始测速"}</strong><span>CSV / TXT / JSON · 可一次拖入多个文件</span></div></div>}
    <aside className="sidebar">
      <div className="brand"><span className="brand-mark"><Zap size={18}/></span><span><strong>CF Node Bench</strong><small>网络节点测量</small></span></div>

      <nav aria-label="主导航">
        <Button variant={page==="run"?"primary":"tertiary"} onPress={()=>setPage("run")}><Activity size={17}/>测速工作台{running&&<span className="live-dot"/>}</Button>
        <Button variant={page==="sources"?"primary":"tertiary"} onPress={()=>setPage("sources")}><Database size={17}/>数据源</Button>
        <Button variant={page==="publish"?"primary":"tertiary"} onPress={()=>setPage("publish")}><Send size={17}/>发布</Button>
        <Button variant={page==="settings"?"primary":"tertiary"} onPress={()=>setPage("settings")}><Settings size={17}/>设置</Button>
      </nav>
      <div className="sidebar-foot">
        <span title={`切换主题：${theme}`}><Button isIconOnly variant="tertiary" aria-label={`当前主题：${theme}`} onPress={nextTheme}>{themeIcon}</Button></span>
        <span>v0.8.1</span>
      </div>
    </aside>
    <main className="main-content">{migrationNotice&&<p className="migration-notice" role="status">{migrationNotice}</p>}{page==="run"?<RunWorkspace/>:page==="sources"?<SourcesPage/>:page==="publish"?<PublishPage/>:<SettingsPage/>}</main>
  </div>;
}
