import { describe, expect, it } from "vitest";
import { can, safeRedirect } from "@/auth/session";
import { type NavItem, visibleNavItems } from "@/components/shell";
import { t } from "@/i18n";
import { user } from "@/test/fake-server";

describe("safeRedirect", () => {
	it.each([
		["/", "/"],
		["/books/42?tab=files#notes", "/books/42?tab=files#notes"],
		[undefined, "/"],
		["", "/"],
		["https://evil.example/", "/"],
		["//evil.example/", "/"],
		["/\\evil.example/", "/"],
		["javascript:alert(1)", "/"],
		["books", "/"],
	])("%s leads to %s", (target, expected) => {
		expect(safeRedirect(target)).toBe(expected);
	});
});

describe("permissions", () => {
	it("follow the role", () => {
		expect(can(user("r", "reader"), "library:read")).toBe(true);
		expect(can(user("r", "reader"), "books:upload")).toBe(false);
		expect(can(user("e", "editor"), "books:upload")).toBe(true);
		expect(can(user("e", "editor"), "users:manage")).toBe(false);
		expect(can(user("a", "admin"), "users:manage")).toBe(true);
		expect(can(null, "library:read")).toBe(false);
	});

	it("decide which navigation entries are offered", () => {
		const items: NavItem[] = [
			{ to: "/", label: "nav.library", permission: "library:read" },
			{ to: "/", label: "nav.main", permission: "books:upload" },
			{ to: "/", label: "nav.skip", permission: "users:manage" },
		];
		const labels = (role: "reader" | "editor" | "admin") =>
			visibleNavItems(items, user("x", role)).map((item) => item.label);

		expect(labels("reader")).toEqual(["nav.library"]);
		expect(labels("editor")).toEqual(["nav.library", "nav.main"]);
		expect(labels("admin")).toEqual(["nav.library", "nav.main", "nav.skip"]);
		expect(visibleNavItems(items, null)).toEqual([]);
	});
});

describe("t", () => {
	it("fills placeholders and leaves unknown ones visible", () => {
		expect(t("user.signedInAs", { name: "Steve" })).toBe("Signed in as Steve");
		expect(t("user.signedInAs")).toBe("Signed in as {name}");
	});
});
