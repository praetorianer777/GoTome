import { locale, t } from "@/i18n";

const formats = new Map<string, Intl.NumberFormat | Intl.DateTimeFormat>();

// Every formatter is made for the language chosen when it is first asked
// for, so a change of language needs no reload.
function numberFormat(options: Intl.NumberFormatOptions): Intl.NumberFormat {
	const key = `n:${locale()}:${JSON.stringify(options)}`;
	let format = formats.get(key) as Intl.NumberFormat | undefined;
	if (!format) {
		format = new Intl.NumberFormat(locale(), options);
		formats.set(key, format);
	}
	return format;
}

function dateFormat(options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
	const key = `d:${locale()}:${JSON.stringify(options)}`;
	let format = formats.get(key) as Intl.DateTimeFormat | undefined;
	if (!format) {
		format = new Intl.DateTimeFormat(locale(), options);
		formats.set(key, format);
	}
	return format;
}

/** A count or a measure as the language writes it: 12,345 or 12.345, 1.5 or 1,5. */
export function formatCount(value: number, fractionDigits = 1): string {
	return numberFormat({ maximumFractionDigits: fractionDigits }).format(value);
}

const UNITS = ["B", "KB", "MB", "GB", "TB"];

/** A file size as people read it: 1.4 MB. */
export function formatSize(bytes: number): string {
	let value = bytes;
	let unit = 0;
	while (value >= 1000 && unit < UNITS.length - 1) {
		value /= 1000;
		unit++;
	}
	return `${formatCount(value)} ${UNITS[unit]}`;
}

/** A length as people read it: 11 h 5 min. */
export function formatDuration(ms: number): string {
	const minutes = Math.round(ms / 60000);
	if (minutes === 0) {
		return t("duration.seconds", { seconds: Math.round(ms / 1000) });
	}
	if (minutes < 60) {
		return t("duration.minutes", { minutes });
	}
	return t("duration.hoursMinutes", {
		hours: Math.floor(minutes / 60),
		minutes: minutes % 60,
	});
}

/** A day: 14 Oct 2026, or 14.10.2026. */
export function formatDate(iso: string | Date): string {
	return dateFormat({ dateStyle: "medium" }).format(new Date(iso));
}

/** A moment: the day and the time of day. */
export function formatDateTime(
	iso: string | Date,
	style: "medium" | "short" = "medium",
): string {
	return dateFormat({
		dateStyle: style,
		timeStyle: style === "short" ? "medium" : "short",
	}).format(new Date(iso));
}

/** The time of day, in hours and minutes. */
export function formatTime(iso: string | Date | number): string {
	return dateFormat({ hour: "2-digit", minute: "2-digit" }).format(
		new Date(iso),
	);
}

/** A month of a year, as a chart labels it: Oct 2026. Months are kept in UTC. */
export function formatMonth(iso: string | Date): string {
	return dateFormat({
		month: "short",
		year: "numeric",
		timeZone: "UTC",
	}).format(new Date(iso));
}

// Release dates are days, not moments: read and written in UTC, so that no
// time zone moves one to the day before.
const RELEASE_OPTIONS: Record<
	"day" | "month" | "year",
	Intl.DateTimeFormatOptions
> = {
	day: { dateStyle: "medium", timeZone: "UTC" },
	month: { month: "long", year: "numeric", timeZone: "UTC" },
	year: { year: "numeric", timeZone: "UTC" },
};

/** A release date as exactly as it is known: a day, a month or a year. */
export function formatReleaseDate(
	date: string,
	precision: "day" | "month" | "year",
): string {
	return dateFormat(RELEASE_OPTIONS[precision]).format(
		new Date(`${date}T00:00:00Z`),
	);
}

/** A language tag as the name of the language, in the language chosen, where the browser knows it. */
export function formatLanguage(tag: string): string {
	try {
		return new Intl.DisplayNames(locale(), { type: "language" }).of(tag) ?? tag;
	} catch {
		return tag;
	}
}
