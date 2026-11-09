import { Icon } from "@/components/shell";
import { t } from "@/i18n";

const AUDIO = ["m4b", "m4a", "mp3", "flac", "ogg", "opus"];

export type Medium = "ebook" | "audio" | "both";

/** What a book is by its files' formats; none for a book without files. */
export function mediumOf(formats: string[]): Medium | undefined {
	const audio = formats.some((f) => AUDIO.includes(f));
	const ebook = formats.some((f) => !AUDIO.includes(f));
	if (audio && ebook) return "both";
	if (audio) return "audio";
	return ebook ? "ebook" : undefined;
}

/**
 * The round badge on a cover's corner. It sits beside the card's link, not in
 * it, so the link keeps its name; the badge says the medium in words.
 */
export function MediumBadge({
	formats,
	className = "",
}: {
	formats: string[];
	className?: string;
}) {
	const medium = mediumOf(formats);
	if (!medium) return null;
	return (
		<span
			role="img"
			aria-label={t(`medium.${medium}`)}
			title={t(`medium.${medium}`)}
			className={`pointer-events-none absolute z-10 flex items-center gap-0.5 rounded-full bg-slate-950 px-1.5 py-1 text-brand shadow-book ring-2 ring-brand/70 ${className}`}
		>
			{medium !== "audio" && <Icon name="book" className="size-4" />}
			{medium !== "ebook" && <Icon name="headphones" className="size-4" />}
		</span>
	);
}
