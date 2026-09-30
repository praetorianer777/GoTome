import { t } from "@/i18n";

const sizeFormat = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
const UNITS = ["B", "KB", "MB", "GB", "TB"];

/** A file size as people read it: 1.4 MB. */
export function formatSize(bytes: number): string {
	let value = bytes;
	let unit = 0;
	while (value >= 1000 && unit < UNITS.length - 1) {
		value /= 1000;
		unit++;
	}
	return `${sizeFormat.format(value)} ${UNITS[unit]}`;
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

const dateFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

export function formatDate(iso: string): string {
	return dateFormat.format(new Date(iso));
}

/** A language tag as the name of the language, where the browser knows it. */
export function formatLanguage(tag: string): string {
	try {
		return new Intl.DisplayNames(undefined, { type: "language" }).of(tag) ?? tag;
	} catch {
		return tag;
	}
}
