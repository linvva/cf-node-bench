import { Tabs } from "@heroui/react";
import type { Mode } from "../types";

export function ProtocolTabs({label,value,onChange,disabled=false}:{label:string;value:Mode;onChange:(value:Mode)=>void;disabled?:boolean}){
  return <Tabs className="mode-tabs protocol-tabs" variant="secondary" selectedKey={value} onSelectionChange={key=>onChange(key as Mode)}><Tabs.ListContainer><Tabs.List aria-label={label}><Tabs.Tab id="tcp_https" isDisabled={disabled}>普通（TCP/HTTPS）</Tabs.Tab><Tabs.Tab id="h3" isDisabled={disabled}>H3</Tabs.Tab></Tabs.List></Tabs.ListContainer><Tabs.Panel className="sr-only" id="tcp_https">普通（TCP/HTTPS）</Tabs.Panel><Tabs.Panel className="sr-only" id="h3">H3</Tabs.Panel></Tabs>;
}
