import { useEffect, useState } from "react";
import cheer from "@/assets/gopher-cheer.webp";
import confused from "@/assets/gopher-confused.webp";
import reading from "@/assets/gopher-reading.webp";
import search from "@/assets/gopher-search.webp";
import sleep from "@/assets/gopher-sleep.webp";
import { t } from "@/i18n";

export type Mood = "sleep" | "search" | "confused" | "reading" | "cheer";

const PICTURES: Record<Mood, string> = { sleep, search, confused, reading, cheer };

/** The gopher in one of its moods. Decoration: what it says is the text beside it. */
export function Gopher({
	mood,
	size = 128,
	className = "",
}: {
	mood: Mood;
	size?: number;
	className?: string;
}) {
	return (
		<img
			src={PICTURES[mood]}
			alt=""
			width={size}
			height={size}
			className={`object-contain ${className}`}
			style={{ width: size, height: "auto", maxHeight: size * 1.3 }}
		/>
	);
}

const SHOW_MS = 4000;
const listeners = new Set<() => void>();

/** Shows the celebration of a finished book, in whichever shell is open. */
export function celebrate() {
	for (const listener of listeners) listener();
}

const STARS = [
	{ x: -90, y: -70, delay: 0, size: 18 },
	{ x: 70, y: -90, delay: 60, size: 14 },
	{ x: -50, y: -110, delay: 120, size: 12 },
	{ x: 100, y: -40, delay: 40, size: 16 },
	{ x: -120, y: -20, delay: 100, size: 12 },
	{ x: 20, y: -120, delay: 160, size: 18 },
	{ x: 120, y: -90, delay: 80, size: 11 },
	{ x: -30, y: -60, delay: 200, size: 14 },
];

/**
 * A toast that goes by itself: the gopher cheering over a few stars. The
 * stars move only where motion is not reduced (`confetti` in styles).
 */
export function Celebration() {
	const [shown, setShown] = useState(0);
	useEffect(() => {
		const show = () => setShown((n) => n + 1);
		listeners.add(show);
		return () => void listeners.delete(show);
	}, []);
	useEffect(() => {
		if (!shown) return;
		const timer = setTimeout(() => setShown(0), SHOW_MS);
		return () => clearTimeout(timer);
	}, [shown]);
	if (!shown) return null;
	return (
		<div
			key={shown}
			role="status"
			className="celebration panel-solid pointer-events-none fixed right-4 bottom-4 z-50 flex items-center gap-3 rounded-2xl py-3 pr-5 pl-3 max-sm:right-2 max-sm:bottom-2"
		>
			<div className="relative size-24 shrink-0">
				<Gopher mood="cheer" size={96} />
				{STARS.map((star) => (
					<span
						key={`${star.x}${star.y}`}
						aria-hidden="true"
						className="confetti absolute top-1/2 left-1/2 text-cream"
						style={
							{
								"--x": `${star.x}px`,
								"--y": `${star.y}px`,
								animationDelay: `${star.delay}ms`,
								fontSize: star.size,
							} as React.CSSProperties
						}
					>
						★
					</span>
				))}
			</div>
			<div>
				<p className="font-serif text-lg font-medium dark:text-cream">
					{t("celebrate.title")}
				</p>
				<p className="text-sm text-slate-600 dark:text-slate-400">
					{t("celebrate.body")}
				</p>
			</div>
		</div>
	);
}
