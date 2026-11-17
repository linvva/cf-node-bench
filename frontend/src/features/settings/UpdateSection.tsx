import { Button, toast } from "@heroui/react";
import { Download, RefreshCw, RotateCw } from "lucide-react";
import { actions, updateBusy, useAppStore } from "../../store";

const bytes=(value:number)=>`${(value/1048576).toFixed(1)} MB`;

export function UpdateSection(){
  const update=useAppStore(state=>state.update);
  const busy=useAppStore(updateBusy);
  const restarting=useAppStore(state=>state.restartingUpdate);
  const {status,version,currentVersion,error,downloaded=0,size=0,notes}=update;
  const message=status==="checking"?"正在检查更新…":status==="available"?`发现新版本 ${version}`:status==="downloading"?`正在下载 ${bytes(downloaded)}${size?` / ${bytes(size)}`:""}`:status==="ready"?`版本 ${version} 已准备就绪`:status==="error"?"更新失败":status==="unavailable"?"当前无法自动更新":"已是最新版本";
  return <section id="app-update" className="panel app-update" aria-labelledby="update-title">
    <div className="update-heading"><div><h2 id="update-title">应用更新</h2><p>当前版本：{currentVersion==="dev"?"开发版":currentVersion}</p></div>
      {status==="available"?<Button size="sm" variant="primary" onPress={()=>void actions.downloadUpdate()}><Download size={14}/>下载更新</Button>:status==="ready"?<Button size="sm" variant="primary" isDisabled={busy||restarting} onPress={()=>void actions.restartForUpdate().catch(error=>toast.danger(String(error)))}><RotateCw size={14}/>{restarting?"正在重启…":"重启并更新"}</Button>:<Button size="sm" variant="secondary" isDisabled={status==="checking"||status==="downloading"||status==="unavailable"} onPress={()=>void actions.checkForUpdate()}><RefreshCw size={14}/>{status==="error"?"重试检查":"检查更新"}</Button>}
    </div>
    <p className="update-status" role={error?"alert":"status"}>{message}{error&&<span>{error}</span>}{status==="ready"&&busy&&<span>测速或发布任务结束后可重启更新</span>}</p>
    {status==="downloading"&&<progress aria-label="更新下载进度" max={size||undefined} value={size?Math.min(downloaded,size):undefined}/>}
    {notes&&<details className="update-notes"><summary>更新说明</summary><p>{notes}</p></details>}
  </section>;
}
