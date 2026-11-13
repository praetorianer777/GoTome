/** Whether this browser can keep books: it needs the Cache API and a service
 * worker, which a page not served over HTTPS (or from localhost) does not
 * get. */
export function offlineSupported(): boolean {
	try {
		return typeof caches !== "undefined";
	} catch {
		return false;
	}
}

export function workerSupported(): boolean {
	return offlineSupported() && "serviceWorker" in navigator;
}
