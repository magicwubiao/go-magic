import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import "./index.css";
import App from "./App";
import { SystemActionsProvider } from "./contexts/SystemActions";
import { exposePluginSDK } from "./plugins";
import { MAGIC_BASE_PATH } from "./lib/api";

exposePluginSDK();

createRoot(document.getElementById("root")!).render(
  <BrowserRouter basename={MAGIC_BASE_PATH || undefined}>
    <SystemActionsProvider>
      <App />
    </SystemActionsProvider>
  </BrowserRouter>,
);