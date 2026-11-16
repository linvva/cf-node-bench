import { afterEach, expect, it, vi } from "vitest";
import type { RunProgress, RunSummary } from "../src/types";

afterEach(()=>{vi.useRealTimers();vi.resetModules();});

it.each([false,true])("refills past the initial HTTP batch until the download target is reached (H3=%s)",async(requireH3)=>{
  vi.resetModules();vi.useFakeTimers();
  const {bridge,defaultSettings}=await import("../src/lib/bridge");
  await bridge.saveSettings({...defaultSettings,requireH3,retainPreviousResults:false,httpCandidateCount:2,bandwidthCandidates:6,finalResultCount:3});
  let summary:RunSummary|undefined;
  const updates:RunProgress[]=[];
  const offProgress=bridge.onProgress(value=>updates.push(value));
  const offComplete=bridge.onComplete(value=>{summary=value;});
  await bridge.startRun({startStage:"source",allowStaleCheckpoint:false});
  await vi.advanceTimersByTimeAsync(6000);
  const httpName=requireH3?"http3":"https";
  const beforeDownload=updates.flatMap(value=>value.stages).find(stage=>stage.name===httpName&&stage.state==="completed");
  const finalStages=updates.at(-1)!.stages;
  const http=finalStages.find(stage=>stage.name===httpName)!;
  const download=finalStages.find(stage=>stage.name==="bandwidth")!;
  expect(beforeDownload!.selected).toBeLessThan(http.selected!);
  expect(download.queue!.pool).toBeGreaterThan(2);
  expect(http.selected).toBe(download.queue!.pool);
  expect(download.queue!.passed).toBe(6);
  expect(download.queue!.planned).toBeLessThanOrEqual(download.queue!.pool);
  expect(summary!.results).toHaveLength(3);
  if(!requireH3)expect(download.failed).toBe(1);
  offProgress();offComplete();
});

it.each([false,true])("finishes short of the target only after the remaining HTTP candidates are exhausted (H3=%s)",async(requireH3)=>{
  vi.resetModules();vi.useFakeTimers();
  const {bridge,defaultSettings}=await import("../src/lib/bridge");
  await bridge.saveSettings({...defaultSettings,requireH3,retainPreviousResults:false,httpCandidateCount:2,bandwidthCandidates:500,finalResultCount:100});
  let final:RunProgress|undefined;
  const off=bridge.onProgress(value=>{final=value;});
  await bridge.startRun({startStage:"source",allowStaleCheckpoint:false});
  await vi.advanceTimersByTimeAsync(6000);
  const http=final!.stages.find(stage=>stage.name===(requireH3?"http3":"https"))!;
  const download=final!.stages.find(stage=>stage.name==="bandwidth")!;
  expect(download.queue!.pool).toBe(http.passed);
  expect(download.queue!.planned).toBe(http.passed);
  expect(download.queue!.passed).toBeLessThan(500);
  expect(download.queue!.active).toBe(0);
  expect(download.queue!.pending).toBe(0);
  expect(download.state).toBe("completed");
  off();
});
