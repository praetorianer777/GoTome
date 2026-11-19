/**
 * foliate-js opens a book's external links with a plain window.open, which
 * hands the page the book chose a handle on this tab and accepts any scheme.
 * Taking the event over, only http(s) is opened, cut off from the app.
 */
export function guardExternalLinks(
	view: EventTarget,
	open: (url: string, target: string, features: string) => unknown = (...a) => window.open(...a),
): void {
	view.addEventListener("external-link", (e) => {
		e.preventDefault();
		const href = (e as CustomEvent<{ href_?: string }>).detail?.href_;
		let url: URL;
		try {
			url = new URL(href ?? "");
		} catch {
			return;
		}
		if (url.protocol !== "http:" && url.protocol !== "https:") return;
		open(url.href, "_blank", "noopener,noreferrer");
	});
}
