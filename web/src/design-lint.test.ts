// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// The rules of docs/design.md that a line of source can break: the type
// scale, the spacing steps and arbitrary values (#378). A deliberate
// exception is listed here with its file, so a new one is a decision in a
// diff and not a habit.
const SRC = fileURLToPath(new URL(".", import.meta.url));

function* sources(dir: string): Generator<string> {
	for (const entry of readdirSync(dir, { withFileTypes: true })) {
		const path = join(dir, entry.name);
		if (entry.isDirectory()) {
			if (entry.name === "test" || entry.name === "fake") continue;
			yield* sources(path);
		} else if (/\.tsx$/.test(entry.name) && !/\.test\.tsx$/.test(entry.name)) {
			yield path;
		}
	}
}

interface Hit {
	file: string;
	line: number;
	token: string;
}

function find(pattern: RegExp, allowed: Record<string, string[]> = {}): string[] {
	const hits: Hit[] = [];
	for (const path of sources(SRC)) {
		const file = relative(SRC, path);
		readFileSync(path, "utf8")
			.split("\n")
			.forEach((text, i) => {
				for (const m of text.matchAll(pattern)) {
					const token = m[0].replace(/^[\s"'`]+/, "");
					if (allowed[file]?.includes(token)) continue;
					hits.push({ file, line: i + 1, token });
				}
			});
	}
	return hits.map((h) => `${h.file}:${h.line} ${h.token}`);
}

// A utility starts after a space, a quote or a variant's colon.
const START = String.raw`(?:^|(?<=[\s"'\`:]))`;

describe("the design rules in the source", () => {
	it("sets text only in the scale", () => {
		// page title 2xl, section lg, body base, controls sm, footnotes xs; the
		// exceptions are the book's title, the 404 heading, stars and figures.
		const scale = new Set(["xs", "sm", "base", "lg", "2xl"]);
		const found = find(new RegExp(`${START}text-(xs|sm|base|lg|[0-9]?xl|\\[[^\\]]*\\])(?![\\w-])`, "g"), {
			"routes/book.tsx": ["text-3xl", "text-4xl"],
			"routes/not-found.tsx": ["text-3xl"],
			"books/reading-controls.tsx": ["text-xl"],
			"routes/overview.tsx": ["text-xl"],
		}).filter((hit) => !scale.has(hit.split("text-")[1] ?? ""));
		expect(found).toEqual([]);
	});

	it("keeps spacing to the 4-pixel steps and the half steps inside controls", () => {
		const spacing = String.raw`-?(?:p[xytblrse]?|m[xytblrse]?|gap(?:-[xy])?|space-[xy]|inset(?:-[xy])?|size|w|h|min-w|min-h|max-w|max-h|top|right|bottom|left)`;
		const half = new RegExp(`${START}${spacing}-(\\d+)\\.5(?![\\w.-])`, "g");
		const bad = find(half).filter((hit) => !/-(0|1|2)\.5$/.test(hit));
		expect(bad, "only 0.5, 1.5 and 2.5 are steps").toEqual([]);
		const quarter = new RegExp(`${START}${spacing}-\\d+\\.(?!5(?![\\d]))\\d+`, "g");
		expect(find(quarter)).toEqual([]);
		const between = find(new RegExp(`${START}space-[xy]-\\d+\\.5(?![\\w.-])`, "g"));
		expect(between, "the space between blocks is never a half step").toEqual([]);
	});

	it("uses no arbitrary value for a size, a space or a type", () => {
		const arbitrary = new RegExp(
			`${START}(?:[a-z-]+:)*(?:-?(?:p[xytblrse]?|m[xytblrse]?|gap(?:-[xy])?|space-[xy])|text|leading|tracking|font)-\\[[^\\]]+\\]`,
			"g",
		);
		expect(find(arbitrary)).toEqual([]);
		const pixels = new RegExp(`${START}(?:[a-z-]+:)*[a-z-]+-\\[[^\\]]*\\d+px[^\\]]*\\]`, "g");
		expect(
			find(pixels, {
				"books/cover.tsx": ["rounded-[3px]"],
				"components/shell.tsx": ["h-[18px]", "w-[18px]"],
			}),
			"a length in pixels outside the steps",
		).toEqual([]);
	});
});
