import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "@/app";
import { applyLocale } from "@/i18n";
import { applyTheme, storedTheme } from "@/lib/theme";
import "@/styles/index.css";

// Before the first paint, so a chosen theme does not flash the other one.
applyTheme(storedTheme());
applyLocale();

async function start() {
	// Written out here, not through isDemo, so the bundler sees a constant and
	// leaves the fake server out of every other build.
	if (import.meta.env.MODE === "demo") {
		await (await import("@/demo/boot")).bootDemo();
	}
	createRoot(document.getElementById("root")!).render(
		<StrictMode>
			<App />
		</StrictMode>,
	);
}

void start();
