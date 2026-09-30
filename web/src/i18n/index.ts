import { en, type MessageKey } from "./en";

export type { MessageKey };

/**
 * The text for a key, with {placeholders} filled in. English is the only
 * language so far; the keys are what a translation will be written against.
 */
export function t(
	key: MessageKey,
	values: Record<string, string | number> = {},
): string {
	return en[key].replace(/\{(\w+)\}/g, (placeholder, name: string) =>
		name in values ? String(values[name]) : placeholder,
	);
}
