import { useEffect, useState } from "react";
import { Button } from "@heroui/react";
import { Activity, Database, Moon, Settings, Sun, SunMoon, Zap } from "lucide-react";
import { actions, useAppStore } from "./store";
import { RunWorkspace } from "./features/run/RunWorkspace";
import { SourcesPage } from "./features/sources/SourcesPage";
import { SettingsPage } from "./features/settings/SettingsPage";

type Page="run"|"sources"|"settings";
type Theme="system"|"light"|"dark";

export function App(){
  const ready=useAppStore(state=>state.ready);
  const running=useAppStore(state=>state.running);
  const [page,setPage]=useState<Page>("run");
  const [theme,setTheme]=useState<Theme>(()=>(localStorage.getItem("theme") as Theme)||"system");
  useEffect(()=>{void actions.init();},[]);
  useEffect(()=>{
    const media=matchMedia("(prefers-color-scheme: dark)");
    const apply=()=>document.documentElement.classList.toggle("dark",theme==="dark"||(theme==="system"&&media.matches));
    apply(); media.addEventListener("change",apply); localStorage.setItem("theme",theme); return()=>media.removeEventListener("change",apply);
  },[theme]);
  const themeIcon=theme==="system"?<SunMoon size={17}/>:theme==="dark"?<Moon size={17}/>:<Sun size={17}/>;
  const nextTheme=()=>setTheme(value=>value==="system"?"light":value==="light"?"dark":"system");

  if(!ready) return <div className="loading-shell"><Zap size={24}/><span>正在载入工作台</span></div>;
  return <div className="app-shell">
    <aside className="sidebar">
      <div className="brand"><span className="brand-mark"><Zap size={18}/></span><span><strong>CF Node Bench</strong><small>网络节点测量</small></span></div>
      <nav aria-label="主导航">
        <Button variant={page==="run"?"primary":"tertiary"} onPress={()=>setPage("run")}><Activity size={17}/>测速工作台{running&&<span className="live-dot"/>}</Button>
        <Button variant={page==="sources"?"primary":"tertiary"} onPress={()=>setPage("sources")}><Database size={17}/>数据源</Button>
        <Button variant={page==="settings"?"primary":"tertiary"} onPress={()=>setPage("settings")}><Settings size={17}/>设置</Button>
      </nav>
      <div className="sidebar-foot">
        <span title={`切换主题：${theme}`}><Button isIconOnly variant="tertiary" aria-label={`当前主题：${theme}`} onPress={nextTheme}>{themeIcon}</Button></span>
        <span>v0.1.0 MVP</span>
      </div>
    </aside>
    <main className="main-content">{page==="run"?<RunWorkspace/>:page==="sources"?<SourcesPage/>:<SettingsPage/>}</main>
  </div>;
}
