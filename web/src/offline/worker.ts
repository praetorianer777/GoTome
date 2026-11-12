import { serverPath } from "@/base";
import { workerSupported } from "./support";

/** Registers the service worker, which the build writes beside index.html.
 * The development server and the demo have none. */
export function registerWorker() {
	if (!import.meta.env.PROD || import.meta.env.MODE === "demo" || !workerSupported()) {
		return;
	}
	navigator.serviceWorker
		.register(serverPath("/sw.js"), { scope: serverPath("/") })
		.catch(() => {
			// Without a worker the app works as before, only not offline.
		});
}

/**
 * Asks the worker to keep all of the app's files, so that the readers, whose
 * code loads with the first book opened, open offline too. Gives up after
 * two minutes; the books are kept either way.
 */
export async function warmApp(): Promise<void> {
	if (!import.meta.env.PROD || !workerSupported()) return;
	const registration = await Promise.race([
		navigator.serviceWorker.ready,
		new Promise<undefined>((resolve) => setTimeout(resolve, 5_000)),
	]);
	const active = registration?.active;
	if (!active) return;
	await new Promise<void>((resolve) => {
		const channel = new MessageChannel();
		channel.port1.onmessage = () => resolve();
		active.postMessage({ type: "warm" }, [channel.port2]);
		setTimeout(resolve, 120_000);
	});
}
