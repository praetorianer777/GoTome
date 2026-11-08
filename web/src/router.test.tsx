import { QueryClient } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { expect, it } from "vitest";
import { makeRouter } from "@/router";

it("asks for no page module again once every page has loaded", async () => {
	const router = makeRouter(
		new QueryClient(),
		createMemoryHistory({ initialEntries: ["/"] }),
	);
	const routes = Object.values(router.routesById);
	for (const route of routes) {
		await route.options.component?.preload?.();
	}
	// A preload left in place is awaited on every navigation, even one that
	// only changes the address's search, so a ticked filter lags (#370).
	const left = routes
		.filter((route) => route.options.component?.preload)
		.map((route) => route.id);
	expect(left).toEqual([]);
});
