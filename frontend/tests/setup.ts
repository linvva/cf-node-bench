import "@testing-library/jest-dom/vitest";
import { vi } from "vitest";

vi.mock("echarts/core",()=>({use:vi.fn(),init:()=>({setOption:vi.fn(),resize:vi.fn(),dispose:vi.fn()})}));

class ResizeObserverMock { observe(){} unobserve(){} disconnect(){} }
Object.defineProperty(globalThis,"ResizeObserver",{value:ResizeObserverMock});
Object.defineProperty(globalThis,"matchMedia",{value:()=>({matches:false,addEventListener(){},removeEventListener(){}})});
Object.defineProperty(navigator,"clipboard",{value:{writeText:async()=>undefined}});
