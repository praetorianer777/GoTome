import { NIGHT, type Night } from "@/player/sleep";

const BOOST_KEY = "gotome.player.boost";
const QUIET_KEY = "gotome.player.nightQuiet";
const NIGHT_KEY = "gotome.player.night";

/** The volume boosts offered, 1 being none. */
export const BOOSTS = [1, 1.25, 1.5, 1.75, 2];

function read(key: string): string | null {
	try {
		return localStorage.getItem(key);
	} catch {
		return null;
	}
}

function write(key: string, value: string) {
	try {
		localStorage.setItem(key, value);
	} catch {
		// Not kept; it holds for this visit.
	}
}

/** The boost this browser chose, 1 when none or not one on offer. */
export function storedBoost(): number {
	const boost = Number(read(BOOST_KEY));
	return BOOSTS.includes(boost) ? boost : 1;
}

export function storeBoost(boost: number) {
	write(BOOST_KEY, String(boost));
}

/** The hours this browser starts a sleep timer in by itself. */
export function storedNight(): Night {
	try {
		const night = JSON.parse(read(NIGHT_KEY) ?? "null") as Partial<Night> | null;
		const clock = /^\d\d:\d\d$/;
		if (
			night &&
			typeof night.enabled === "boolean" &&
			clock.test(night.from ?? "") &&
			clock.test(night.to ?? "") &&
			Number.isFinite(night.minutes) &&
			(night.minutes ?? 0) > 0
		) {
			return night as Night;
		}
	} catch {
		// Unreadable: as if none was chosen.
	}
	return NIGHT;
}

/** Until when a deliberate "off" keeps the night timer from starting again. */
export function storedQuiet(): number {
	return Number(read(QUIET_KEY)) || 0;
}

export function storeQuiet(until: number) {
	write(QUIET_KEY, String(until));
}

export function storeNight(night: Night) {
	write(NIGHT_KEY, JSON.stringify(night));
}
