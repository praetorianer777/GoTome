import badgeAudio from "@/assets/badge-audio.webp";
import badgeBoth from "@/assets/badge-both.webp";
import badgeEbook from "@/assets/badge-ebook.webp";
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

const BADGES: Record<Medium, string> = {
	ebook: badgeEbook,
	audio: badgeAudio,
	both: badgeBoth,
};

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
		<img
			src={BADGES[medium]}
			width={28}
			height={28}
			alt={t(`medium.${medium}`)}
			title={t(`medium.${medium}`)}
			className={`pointer-events-none absolute z-10 size-7 rounded-full shadow-book ${className}`}
		/>
	);
}
