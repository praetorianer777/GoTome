import { useEffect, useRef, useState } from "react";
import type { MessageKey } from "@/i18n";

/** A key a view answers, with what it does, for the view's list of keys. */
export interface KeyBinding {
	/**
	 * The values of KeyboardEvent.key it answers, "Shift+" before one that
	 * needs Shift held. Space, arrows and paging keys without it answer only
	 * without Shift; any other key is what Shift made it, as "?" is.
	 */
	keys: string[];
	/** How the list of keys writes them, where not one by one. */
	display?: string[];
	/** What the list of keys says it does. */
	label: MessageKey;
	run: (e: KeyboardEvent) => void;
}

/** What can take focus inside an element, in the order Tab goes. */
export const FOCUSABLE =
	'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Moves focus into a panel when it opens: to the entry marked current, or
 * the first thing that takes focus. The ref goes on the panel.
 */
export function usePanelFocus(which: unknown) {
	const ref = useRef<HTMLDivElement>(null);
	useEffect(() => {
		const el = ref.current;
		if (which === undefined || which === false || !el) return;
		const focus = () => {
			const target =
				el.querySelector<HTMLElement>("[aria-current]:not([aria-current=false])") ??
				el.querySelector<HTMLElement>(FOCUSABLE);
			target?.focus();
			return Boolean(target);
		};
		if (focus()) return;
		// What the panel lists may arrive after it opens, such as a PDF's outline.
		const observer = new MutationObserver(() => {
			if (focus()) observer.disconnect();
		});
		observer.observe(el, { childList: true, subtree: true });
		return () => observer.disconnect();
	}, [which]);
	return ref;
}

/** Whether a key press is someone typing into a field rather than a command. */
export function typing(e: KeyboardEvent): boolean {
	const el = e.target as HTMLElement | null;
	if (!el?.tagName) return false;
	if (
		el.isContentEditable ||
		el.tagName === "TEXTAREA" ||
		el.tagName === "SELECT"
	)
		return true;
	return (
		el.tagName === "INPUT" &&
		!["checkbox", "radio", "button", "submit"].includes(
			(el as HTMLInputElement).type,
		)
	);
}

// Space and Enter on a focused button or link press it; taken as a command
// as well, they would do two things at once.
function pressing(e: KeyboardEvent): boolean {
	if (e.key !== " " && e.key !== "Enter") return false;
	const el = e.target as HTMLElement | null;
	return Boolean(el?.closest?.("button, a, summary, [role=button]"));
}

const SHIFTABLE = new Set([" ", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "PageUp", "PageDown", "Home", "End"]);

function matches(entry: string, e: KeyboardEvent): boolean {
	if (entry.startsWith("Shift+") && entry.length > 6) {
		return e.shiftKey && e.key === entry.slice(6);
	}
	return e.key === entry && !(e.shiftKey && SHIFTABLE.has(entry));
}

let modals = 0;

/** Keeps a view's keys quiet while a modal dialog is open over it. */
export function useModal(): void {
	useEffect(() => {
		modals++;
		return () => {
			modals--;
		};
	}, []);
}

/** Runs the binding the key press is for; true when there was one. */
export function runKey(e: KeyboardEvent, bindings: KeyBinding[]): boolean {
	if (modals > 0 || e.defaultPrevented || pressing(e)) return false;
	// Escape means nothing to a text field, so it closes what holds one.
	if (typing(e) && e.key !== "Escape") return false;
	if (e.ctrlKey || e.metaKey || e.altKey) return false;
	const binding = bindings.find((b) => b.keys.some((entry) => matches(entry, e)));
	if (!binding) return false;
	e.preventDefault();
	binding.run(e);
	return true;
}

/**
 * Answers the bindings anywhere on the page while the caller is mounted.
 * The handler it returns takes key presses that do not reach the window,
 * such as those inside a book's frames.
 */
export function useKeys(bindings: KeyBinding[]): (e: KeyboardEvent) => void {
	const latest = useRef(bindings);
	latest.current = bindings;
	const [handler] = useState(() => (e: KeyboardEvent) => {
		runKey(e, latest.current);
	});
	useEffect(() => {
		window.addEventListener("keydown", handler);
		return () => window.removeEventListener("keydown", handler);
	}, [handler]);
	return handler;
}
