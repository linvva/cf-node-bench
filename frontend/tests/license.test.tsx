import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { LicenseDialog } from "../src/features/settings/LicenseDialog";
import projectLicense from "../../LICENSE?raw";
import thirdPartyNotices from "../public/THIRD_PARTY_NOTICES.md?raw";
import unicodeLicense from "../public/licenses/Unicode-3.0.txt?raw";

afterEach(()=>{cleanup();vi.restoreAllMocks();});

it("opens each complete license locally and closes the dialog",async()=>{
  const fetch=vi.spyOn(window,"fetch").mockRejectedValue(new Error("offline"));
  const open=vi.spyOn(window,"open");
  render(<LicenseDialog/>);
  fireEvent.click(screen.getByRole("button",{name:"许可证与第三方声明"}));
  const dialog=await screen.findByRole("dialog",{name:"许可证与第三方声明"});
  for(const [title,text] of [["项目许可证",projectLicense],["第三方声明",thirdPartyNotices],["Unicode 许可",unicodeLicense]]){
    fireEvent.click(within(dialog).getByRole("tab",{name:title}));
    expect(within(dialog).getByRole("tabpanel").querySelector("pre")?.textContent).toBe(text);
  }
  expect(within(dialog).queryByRole("link")).not.toBeInTheDocument();
  expect(fetch).not.toHaveBeenCalled();
  expect(open).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button",{name:"关闭许可证与第三方声明"}));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
