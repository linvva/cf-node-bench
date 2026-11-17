import { vi } from "vitest";

type Bindings=typeof import("../src/lib/native").app;
export type NativeApp={[K in keyof Bindings]:(...args:Parameters<Bindings[K]>)=>Promise<Awaited<ReturnType<Bindings[K]>>>};
export function mockNative(api:Partial<NativeApp>,events:Record<string,(value:unknown)=>void>){
  Object.defineProperty(window,"_wails",{configurable:true,writable:true,value:{environment:{OS:"windows"}}});
  vi.doMock("../src/lib/native",()=>({
    app:{UpdateStatus:async()=>({currentVersion:"dev",status:"unavailable"}),...api},
    onEvent:(name:string,handler:(value:unknown)=>void)=>{events[name]=handler;return ()=>{delete events[name];};},
    openURL:vi.fn(),
  }));
}
export function clearNative(){
  delete (window as typeof window&{_wails?:unknown})._wails;
  vi.doUnmock("../src/lib/native");
}
