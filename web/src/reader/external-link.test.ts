import { describe, expect, it, vi } from "vitest";
import { guardExternalLinks } from "./external-link";

function fire(href: string) {
	const view = new EventTarget();
	const open = vi.fn();
	guardExternalLinks(view, open);
	const event = new CustomEvent("external-link", { detail: { href_: href }, cancelable: true });
	view.dispatchEvent(event);
	return { open, event };
}

describe("external links of a book", () => {
	it("opens http(s) without a handle on the app's tab", () => {
		const { open, event } = fire("https://example.org/a?b=1");
		expect(open).toHaveBeenCalledWith("https://example.org/a?b=1", "_blank", "noopener,noreferrer");
		expect(event.defaultPrevented).toBe(true);
	});

	it.each(["javascript:alert(1)", "data:text/html,<b>x</b>", "blob:https://x/1", "file:///etc/passwd", "relative/page.html", ""])(
		"ignores %s",
		(href) => {
			const { open, event } = fire(href);
			expect(open).not.toHaveBeenCalled();
			expect(event.defaultPrevented).toBe(true);
		},
	);
});
