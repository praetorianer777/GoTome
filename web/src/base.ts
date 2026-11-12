/**
 * The path the server serves the app under, such as "/gotome/"; "/" when it
 * has the host to itself. The server writes it into index.html at each
 * request, so one build serves any path.
 */
function readBase(): string {
	if (typeof document === "undefined") return "/";
	const content = document.querySelector('meta[name="gotome-base"]')?.getAttribute("content");
	return content?.startsWith("/") && content.endsWith("/") ? content : "/";
}

export const BASE_PATH = readBase();

/** The base path without its last slash: "" at the root, "/gotome" under one. */
export const BASE_PREFIX = BASE_PATH.slice(0, -1);

/** An address of this server, from the origin's root, under the base path. */
export function serverPath(path: string): string {
	return BASE_PREFIX + path;
}
