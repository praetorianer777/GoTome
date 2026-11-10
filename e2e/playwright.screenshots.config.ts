import { defineConfig, devices } from "@playwright/test";
import { BASE_URL } from "./fixtures/stack";

export default defineConfig({
	testDir: "./screenshots",
	outputDir: "./test-results-screenshots",
	retries: 0,
	timeout: 600_000,
	expect: { timeout: 15_000 },
	reporter: [["list"]],
	use: {
		baseURL: BASE_URL,
		actionTimeout: 15_000,
		navigationTimeout: 30_000,
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
