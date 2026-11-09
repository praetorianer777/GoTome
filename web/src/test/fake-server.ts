import { QueryClient } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { createElement } from "react";
import { vi } from "vitest";
import { App } from "@/app";
import { FakeServer as Server } from "@/fake/server";
import { makeRouter } from "@/router";

export { user } from "@/fake/server";

/** The fake the demo runs on, with fetch and uploads replaced for one test. */
export class FakeServer extends Server {
	install(): this {
		vi.spyOn(globalThis, "fetch").mockImplementation((input) => this.handle(input));
		vi.stubGlobal("XMLHttpRequest", this.uploadRequest());
		return this;
	}
}

// Pages load on demand (router.tsx). Loading every one before the tests keeps
// a test's first visit to a page from waiting on its module being compiled,
// which on a busy machine takes longer than a query waits.
import.meta.glob("../routes/*.tsx", { eager: true });

/** Renders the whole app at an address, against the fake server. */
export function renderApp(path: string) {
	// No retries: a test that expects a failure should not wait for three.
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	const history = createMemoryHistory({ initialEntries: [path] });
	const router = makeRouter(queryClient, history);
	return {
		router,
		queryClient,
		...render(createElement(App, { queryClient, router })),
	};
}
