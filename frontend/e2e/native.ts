import { readFileSync } from "node:fs";
import type { Page } from "@playwright/test";

declare global {
  interface Window {
    __nativeApi:Record<string,(...args:any[])=>any>;
    __emitNative:(name:string,data:unknown)=>void;
  }
}

// Exercise generated RPC bindings and Wails 3 event envelopes using an in-page backend.
export async function installNative(page:Page){
  const source=readFileSync(new URL("../bindings/github.com/linvva/cf-node-bench/app.ts",import.meta.url),"utf8");
  const methods=Object.fromEntries([...source.matchAll(/export function (\w+)\([^]*?\$Call\.ByID\((\d+)/g)].map(match=>[match[2],match[1]]));
  await page.addInitScript(methods=>{
    const host=window as typeof window&{_wails:{environment:unknown;dispatchWailsEvent?:(event:{name:string;data:unknown})=>void}};
    host._wails={environment:{OS:"windows"}};
    let data:Record<string,unknown>={version:"0.9.0"};
    window.__nativeApi={Bootstrap:()=>data,SaveSettings:settings=>{data={...data,settings};},UpdateStatus:()=>({currentVersion:"0.9.0",status:"unavailable"})};
    window.__emitNative=(name,data)=>host._wails.dispatchWailsEvent?.({name,data});
    const fetch=window.fetch.bind(window);
    window.fetch=async(input,init)=>{
      if(new URL(String(input),location.href).pathname!=="/wails/runtime")return fetch(input,init);
      const request=JSON.parse(String(init?.body));
      if(request.object!==0)return new Response("null",{headers:{"Content-Type":"application/json"}});
      const method=methods[request.args.methodID];
      try{
        if(!window.__nativeApi[method])throw new Error(`Unknown RPC: ${method}`);
        const result=await window.__nativeApi[method](...(request.args.args??[]));
        return new Response(JSON.stringify(result??null),{headers:{"Content-Type":"application/json"}});
      }catch(error){return new Response(JSON.stringify({kind:"RuntimeError",message:String(error)}),{status:500,headers:{"Content-Type":"application/json"}});}
    };
  },methods);
}
