import {
	LOCALES,
	type Locale,
	localeChosen,
	type MessageKey,
	setLocale,
	t,
	useLocale,
} from "@/i18n";
import { type Theme, useTheme } from "@/lib/theme";

const select =
	"rounded-md border border-slate-300 bg-white px-2 py-1 text-slate-900 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100";

// Each language is named in itself, so that someone who cannot read the
// one shown finds their own.
const LANGUAGE_NAMES: Record<Locale, string> = { en: "English", de: "Deutsch" };

/** The language of the app, which is each browser's own, like its colours. */
export function LanguageSelect() {
	const current = useLocale();
	const chosen = localeChosen() ? current : "browser";
	return (
		<label className="flex items-center justify-between gap-2">
			<span className="text-slate-600 dark:text-slate-400">
				{t("profile.language")}
			</span>
			<select
				value={chosen}
				onChange={(e) =>
					setLocale(
						e.target.value === "browser" ? null : (e.target.value as Locale),
					)
				}
				className={select}
			>
				<option value="browser">{t("profile.language.browser")}</option>
				{LOCALES.map((l) => (
					<option key={l} value={l} lang={l}>
						{LANGUAGE_NAMES[l]}
					</option>
				))}
			</select>
		</label>
	);
}

const THEMES: { value: Theme; label: MessageKey }[] = [
	{ value: "system", label: "theme.system" },
	{ value: "light", label: "theme.light" },
	{ value: "dark", label: "theme.dark" },
];

export function ThemeSelect() {
	const [theme, setTheme] = useTheme();
	return (
		<label className="flex items-center justify-between gap-2">
			<span className="text-slate-600 dark:text-slate-400">
				{t("theme.label")}
			</span>
			<select
				value={theme}
				onChange={(e) => setTheme(e.target.value as Theme)}
				className={select}
			>
				{THEMES.map((option) => (
					<option key={option.value} value={option.value}>
						{t(option.label)}
					</option>
				))}
			</select>
		</label>
	);
}
