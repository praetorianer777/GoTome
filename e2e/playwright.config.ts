import { defineConfig, devices } from "@playwright/test";
import { BASE_URL } from "./fixtures/stack";

// The push gate runs the tests tagged @smoke in Chromium. CI runs everything
// in every project: make test-e2e FULL=1.
export default defineConfig({
	testDir: "./tests",
	outputDir: "./test-results",
	fullyParallel: true,
	forbidOnly: Boolean(process.env.CI),
	retries: 0,
	timeout: 20_000,
	expect: { timeout: 5_000 },
	reporter: [
		["list"],
		["html", { outputFolder: "playwright-report", open: "never" }],
	],
	use: {
		baseURL: BASE_URL,
		trace: "retain-on-failure",
		screenshot: "only-on-failure",
		actionTimeout: 5_000,
		navigationTimeout: 10_000,
	},
	projects: [
		// Creates the first account through the setup page when the stack is
		// fresh. Every other project needs that account, and only one browser
		// can be the one that creates it.
		{
			name: "first-run",
			testMatch: /.*\.setup\.ts/,
			use: { ...devices["Desktop Chrome"] },
		},
		{
			name: "chromium",
			dependencies: ["first-run"],
			use: { ...devices["Desktop Chrome"] },
		},
		{
			name: "firefox",
			dependencies: ["first-run"],
			use: { ...devices["Desktop Firefox"] },
		},
		{
			name: "webkit",
			dependencies: ["first-run"],
			use: {
				...devices["Desktop Safari"],
				// Without a GPU, WebKit rasterises through Mesa's software GL,
				// which compiles its shaders on first use, and the container
				// starts with no shader cache: the first pages of a run then
				// drew no frame for five seconds and more, and every click
				// timed out waiting for its element to be stable (#122).
				// Skia's CPU rasteriser draws the same pages without that.
				launchOptions: {
					env: { ...process.env, WEBKIT_SKIA_ENABLE_CPU_RENDERING: "1" },
				},
			},
		},
		{
			name: "phone",
			dependencies: ["first-run"],
			use: { ...devices["Pixel 7"] },
		},
	],
});
