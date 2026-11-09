/** Set by the Vite mode `demo`, the build for GitHub Pages: no server, a fake in the browser. */
export const isDemo = import.meta.env.MODE === "demo";

/** Pages the demo says it does not have: they write, or they are the server's. */
const NOT_IN_THE_DEMO = [
	/^\/upload/,
	/^\/admin\//,
	/^\/jobs/,
	/^\/trash/,
	/^\/review/,
	/^\/cleanup/,
	/^\/genres/,
	/^\/bulk\//,
	/^\/books\/[^/]+\/(edit|find)$/,
];

export function notInTheDemo(pathname: string): boolean {
	return isDemo && NOT_IN_THE_DEMO.some((pattern) => pattern.test(pathname));
}
