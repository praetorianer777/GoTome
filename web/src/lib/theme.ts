import { useEffect, useState } from "react";

export type Theme = "system" | "light" | "dark";

const STORAGE_KEY = "gotome.theme";

function isTheme(value: unknown): value is Theme {
	return value === "system" || value === "light" || value === "dark";
}

export function storedTheme(): Theme {
	try {
		const value = localStorage.getItem(STORAGE_KEY);
		return isTheme(value) ? value : "system";
	} catch {
		// Storage can be switched off; the choice then lasts for the visit.
		return "system";
	}
}

/**
 * Puts the choice on the document. "system" removes the attribute, which
 * leaves the stylesheet's prefers-color-scheme rule in charge.
 */
export function applyTheme(theme: Theme): void {
	if (theme === "system") {
		delete document.documentElement.dataset.theme;
	} else {
		document.documentElement.dataset.theme = theme;
	}
}

export function useTheme(): [Theme, (theme: Theme) => void] {
	const [theme, setTheme] = useState<Theme>(storedTheme);

	useEffect(() => {
		applyTheme(theme);
	}, [theme]);

	return [
		theme,
		(next) => {
			try {
				localStorage.setItem(STORAGE_KEY, next);
			} catch {
				// See storedTheme.
			}
			setTheme(next);
		},
	];
}
