import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir:"./e2e",
  fullyParallel:false,
  use:{baseURL:"http://127.0.0.1:34115",headless:true},
  webServer:{command:"node_modules/.bin/vite --host 127.0.0.1 --port 34115",port:34115,reuseExistingServer:true},
  projects:[
    {name:"1280-light",use:{viewport:{width:1280,height:800},colorScheme:"light"}},
    {name:"1440-dark",use:{viewport:{width:1440,height:900},colorScheme:"dark"}},
  ],
});
