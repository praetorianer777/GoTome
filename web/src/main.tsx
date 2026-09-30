import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "@/app";
import { applyTheme, storedTheme } from "@/lib/theme";
import "@/styles/index.css";

// Before the first paint, so a chosen theme does not flash the other one.
applyTheme(storedTheme());

createRoot(document.getElementById("root")!).render(
	<StrictMode>
		<App />
	</StrictMode>,
);
