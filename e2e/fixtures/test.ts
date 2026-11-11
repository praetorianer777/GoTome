import {
	type Browser,
	type BrowserContext,
	type Page,
	test as base,
} from "@playwright/test";

// WebKit now and then answers a navigation with "WebKit encountered an
// internal error" and loads the page when asked again.
const WEBKIT_INTERNAL_ERROR = /WebKit encountered an internal error/;

function retryGoto(page: Page): void {
	const goto = page.goto.bind(page);
	page.goto = async (url, options) => {
		try {
			return await goto(url, options);
		} catch (error) {
			if (!WEBKIT_INTERNAL_ERROR.test(String(error))) throw error;
			return goto(url, options);
		}
	};
}

function retryGotoIn(context: BrowserContext): void {
	for (const page of context.pages()) retryGoto(page);
	context.on("page", retryGoto);
}

/**
 * Playwright's test, whose pages ask again once when WebKit fails a
 * `goto` with an internal error. Every spec takes `test` from here.
 */
export const test = base.extend<{ browser: Browser }>({
	context: async ({ context }, use) => {
		retryGotoIn(context);
		await use(context);
	},
	browser: async ({ browser }, use) => {
		const newContext = browser.newContext.bind(browser);
		browser.newContext = async (options) => {
			const context = await newContext(options);
			retryGotoIn(context);
			return context;
		};
		await use(browser);
		browser.newContext = newContext;
	},
});
