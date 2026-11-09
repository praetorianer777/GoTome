import { defineConfig, devices } from "@playwright/test";

// The demo build (make web-demo) served as GitHub Pages serves it, under
// /GoTome/ (demo/serve.mjs). No stack is involved: the app answers itself.
const PORT = 4173;

export default defineConfig({
	testDir: "./demo",
	outputDir: "./test-results-demo",
	forbidOnly: Boolean(process.env.CI),
	retries: 0,
	timeout: 30_000,
	expect: { timeout: 8_000 },
	reporter: [["list"]],
	use: {
		baseURL: `http://127.0.0.1:${PORT}`,
		trace: "retain-on-failure",
		screenshot: "only-on-failure",
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: {
		command: `node demo/serve.mjs ${PORT}`,
		url: `http://127.0.0.1:${PORT}/GoTome/`,
		reuseExistingServer: false,
	},
});
