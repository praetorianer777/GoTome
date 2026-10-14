import { afterEach, describe, expect, it, vi } from "vitest";
import { de } from "./de";
import { en, type MessageKey } from "./en";
import { browserLocale, locale, setLocale, t } from "./index";

const placeholders = (text: string) =>
	[...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

afterEach(() => {
	setLocale(null);
});

describe("translations", () => {
	it("fill the same placeholders as the English texts", () => {
		for (const key of Object.keys(en) as MessageKey[]) {
			expect(placeholders(de[key]), key).toEqual(placeholders(en[key]));
		}
	});

	it("leave no German text in English", () => {
		const same = (Object.keys(en) as MessageKey[]).filter(
			(key) => de[key] === en[key] && /[a-z]{3,} [a-z]{3,}/.test(en[key]),
		);
		expect(same).toEqual([]);
	});
});

describe("t", () => {
	it("speaks the chosen language and puts it on the document", () => {
		setLocale("de");
		expect(locale()).toBe("de");
		expect(document.documentElement.lang).toBe("de");
		expect(t("login.title")).toBe("Anmelden");
		setLocale("en");
		expect(t("login.title")).toBe("Sign in");
	});

	it("writes numbers as the language does, but not years and pages", () => {
		setLocale("de");
		expect(t("overview.waiting", { count: 12345 })).toBe("12.345 wartend");
		expect(t("filter.decade", { decade: 1990 })).toBe("1990er");
		setLocale("en");
		expect(t("overview.waiting", { count: 12345 })).toBe("12,345 waiting");
	});
});

describe("browserLocale", () => {
	it("takes German for any German the browser asks for, else English", () => {
		vi.spyOn(navigator, "languages", "get").mockReturnValue(["de-AT", "en"]);
		expect(browserLocale()).toBe("de");
		vi.spyOn(navigator, "languages", "get").mockReturnValue(["fr-FR", "en-GB"]);
		expect(browserLocale()).toBe("en");
		vi.spyOn(navigator, "languages", "get").mockReturnValue(["fr-FR"]);
		expect(browserLocale()).toBe("en");
	});
});
