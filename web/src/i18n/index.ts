import { useSyncExternalStore } from "react";
import { de } from "./de";
import { en, type MessageKey } from "./en";

export type { MessageKey };

export type Locale = "en" | "de";

export const LOCALES: Locale[] = ["en", "de"];

const MESSAGES: Record<Locale, Record<MessageKey, string>> = { en, de };

const STORAGE_KEY = "gotome.locale";

function isLocale(value: unknown): value is Locale {
	return value === "en" || value === "de";
}

/** The language the browser asks for: German for any de-*, else English. */
export function browserLocale(): Locale {
	const asked =
		typeof navigator === "undefined"
			? []
			: (navigator.languages ?? [navigator.language]);
	for (const tag of asked) {
		const base = tag?.toLowerCase().split("-")[0];
		if (base === "de") return "de";
		if (base === "en") return "en";
	}
	return "en";
}

/** The language chosen on this browser, or the browser's own. */
export function storedLocale(): Locale {
	try {
		const value = localStorage.getItem(STORAGE_KEY);
		if (isLocale(value)) return value;
	} catch {
		// Storage can be switched off; the browser's language then decides.
	}
	return browserLocale();
}

let current: Locale = storedLocale();
const listeners = new Set<() => void>();

/** The language the app speaks now. */
export function locale(): Locale {
	return current;
}

/** Puts the language on the document, for screen readers and hyphenation. */
export function applyLocale(): void {
	document.documentElement.lang = current;
}

/**
 * Chooses the language for this browser. Null forgets the choice, which
 * leaves the browser's language in charge again.
 */
export function setLocale(next: Locale | null): void {
	try {
		if (next) localStorage.setItem(STORAGE_KEY, next);
		else localStorage.removeItem(STORAGE_KEY);
	} catch {
		// See storedLocale: the choice then lasts for the visit.
	}
	current = next ?? browserLocale();
	applyLocale();
	for (const listener of listeners) listener();
}

/** Whether a language was chosen here rather than taken from the browser. */
export function localeChosen(): boolean {
	try {
		return isLocale(localStorage.getItem(STORAGE_KEY));
	} catch {
		return false;
	}
}

/** The language, re-rendering the caller when it changes. */
export function useLocale(): Locale {
	return useSyncExternalStore(
		(listener) => {
			listeners.add(listener);
			return () => listeners.delete(listener);
		},
		locale,
		locale,
	);
}

const numberFormats = new Map<Locale, Intl.NumberFormat>();

/**
 * A number as the language writes it. Whole numbers under 10,000 are left
 * ungrouped, as years and page numbers are written without a separator.
 */
export function formatNumber(value: number): string {
	if (Number.isInteger(value) && Math.abs(value) < 10_000) return String(value);
	let format = numberFormats.get(current);
	if (!format) {
		format = new Intl.NumberFormat(current, { maximumFractionDigits: 2 });
		numberFormats.set(current, format);
	}
	return format.format(value);
}

/**
 * The text for a key in the chosen language, with {placeholders} filled in.
 * Numbers are written as the language writes them.
 */
export function t(
	key: MessageKey,
	values: Record<string, string | number> = {},
): string {
	return MESSAGES[current][key].replace(
		/\{(\w+)\}/g,
		(placeholder, name: string) => {
			if (!(name in values)) return placeholder;
			const value = values[name];
			return typeof value === "number" ? formatNumber(value) : String(value);
		},
	);
}
