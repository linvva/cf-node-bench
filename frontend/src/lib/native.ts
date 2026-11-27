import { Browser, Events } from "@wailsio/runtime";
import * as App from "../../bindings/github.com/linvva/cf-node-bench/app";
import type { Bootstrap, DownloadCheck, PublishSaveRequest, PublishSettingsView, Settings, UpdateState } from "../types";

// Keep the existing UI models at the JSON boundary: Go bindings include zero enum
// values and legacy score fields that the UI normalizes or leaves out on save.
export const app={
  ...App,
  Bootstrap:()=>App.Bootstrap() as unknown as Promise<Bootstrap>,
  UpdateStatus:()=>App.UpdateStatus() as Promise<UpdateState>,
  CheckForUpdate:()=>App.CheckForUpdate() as Promise<UpdateState>,
  DownloadUpdate:()=>App.DownloadUpdate() as Promise<UpdateState>,
  TestDownloadAddress:(value:Settings)=>App.TestDownloadAddress(value as Parameters<typeof App.TestDownloadAddress>[0]) as unknown as Promise<DownloadCheck>,
  SaveSettings:(value:Settings)=>App.SaveSettings(value as Parameters<typeof App.SaveSettings>[0]),
  SavePublishSettings:(value:PublishSaveRequest)=>App.SavePublishSettings(value as Parameters<typeof App.SavePublishSettings>[0]) as Promise<PublishSettingsView>,
  ClearPublishCredential:(target:string)=>App.ClearPublishCredential(target) as Promise<PublishSettingsView>,
  TestPublishTarget:(target:string,value:PublishSaveRequest)=>App.TestPublishTarget(target,value as Parameters<typeof App.TestPublishTarget>[1]),
};
export const openURL=(url:string)=>Browser.OpenURL(url);
export function onEvent<T>(name:string,handler:(value:T)=>void){
  return Events.On(name,event=>handler(event.data as T));
}
