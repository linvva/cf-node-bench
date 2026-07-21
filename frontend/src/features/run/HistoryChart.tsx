import { useEffect, useRef } from "react";
import * as echarts from "echarts/core";
import { LineChart } from "echarts/charts";
import { GridComponent, TooltipComponent } from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import type { RunSummary } from "../../types";

echarts.use([LineChart,GridComponent,TooltipComponent,CanvasRenderer]);

export function HistoryChart({history}:{history:RunSummary[]}){
  const ref=useRef<HTMLDivElement>(null);
  useEffect(()=>{if(!ref.current)return; const chart=echarts.init(ref.current); const data=history.slice(0,10).reverse(); chart.setOption({animation:false,grid:{left:35,right:15,top:8,bottom:24},tooltip:{trigger:"axis"},xAxis:{type:"category",data:data.map(item=>new Date(item.finishedAt).toLocaleTimeString("zh-CN",{hour:"2-digit",minute:"2-digit"})),axisLabel:{fontSize:9,color:"#7b8188"},axisLine:{lineStyle:{color:"#d8dade"}}},yAxis:{type:"value",min:0,max:100,axisLabel:{fontSize:9,color:"#7b8188"},splitLine:{lineStyle:{color:"#e7e8ea"}}},series:[{type:"line",smooth:true,symbolSize:5,lineStyle:{width:2,color:"#0f8f85"},itemStyle:{color:"#0f8f85"},areaStyle:{opacity:.05,color:"#0f8f85"},data:data.map(item=>item.results[0]?.score||0)}]}); const resize=new ResizeObserver(()=>chart.resize()); resize.observe(ref.current); return()=>{resize.disconnect();chart.dispose();};},[history]);
  return <section className="panel history-band"><h2>最近运行 · 最高综合分</h2><div ref={ref} className="chart-wrap" aria-label="历史最高分折线图"/></section>;
}
