import night from "@/assets/wordmark.webp";
import nightLarge from "@/assets/wordmark-large.webp";
import day from "@/assets/wordmark-light.webp";
import dayLarge from "@/assets/wordmark-light-large.webp";
import { t } from "@/i18n";

const IMAGES = {
	small: { night, day, width: 129, height: 36 },
	large: { night: nightLarge, day: dayLarge, width: 280, height: 79 },
};

/**
 * The name as the artwork writes it. An image, one for each theme: only the
 * one of the theme in use is shown, and the other, hidden, is not loaded.
 */
export function Wordmark({
	size = "small",
	className = "",
}: {
	size?: "small" | "large";
	className?: string;
}) {
	const img = IMAGES[size];
	const shared = {
		width: img.width,
		height: img.height,
		alt: t("app.name"),
		loading: "lazy" as const,
		decoding: "async" as const,
	};
	return (
		<>
			<img
				{...shared}
				src={img.day}
				className={`${className} h-auto dark:hidden`}
			/>
			<img
				{...shared}
				src={img.night}
				className={`${className} hidden h-auto dark:block`}
			/>
		</>
	);
}
