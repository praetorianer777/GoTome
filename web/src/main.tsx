import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "@/app";
import { applyLocale, loadLocale, locale } from "@/i18n";
import { applyTheme, storedTheme } from "@/lib/theme";
import "@/styles/index.css";

// Before the first paint, so a chosen theme does not flash the other one.
applyTheme(storedTheme());

async function start() {
	// German is a chunk of its own; the first render waits for it so that no
	// English shows first. A failed fetch leaves English.
	await loadLocale(locale()).catch(() => {});
	applyLocale();
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
