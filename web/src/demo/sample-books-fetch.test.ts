import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";
import {
	describeError,
	fetchBook,
	obtainBook,
	sha256,
} from "../../scripts/sample-books-fetch.mjs";

const ok = (text: string) =>
	({ ok: true, status: 200, arrayBuffer: async () => Buffer.from(text) }) as unknown as Response;
const status = (code: number) => ({ ok: false, status: code }) as unknown as Response;
const quiet = { sleep: async () => {}, log: () => {} };

describe("describeError", () => {
	it("names the cause fetch hides", () => {
		const err = new TypeError("fetch failed", {
			cause: Object.assign(new Error("connect timed out"), { code: "ETIMEDOUT" }),
		});
		expect(describeError(err)).toBe("fetch failed; cause: ETIMEDOUT connect timed out");
	});
});

describe("fetchBook", () => {
	it("tries again with a growing pause and then succeeds", async () => {
		const fetchFn = vi
			.fn()
			.mockRejectedValueOnce(new TypeError("fetch failed"))
			.mockResolvedValueOnce(status(503))
			.mockResolvedValueOnce(ok("book"));
		const pauses: number[] = [];
		const data = await fetchBook("https://x/a.epub", {
			fetchFn,
			pauseMs: 100,
			log: () => {},
			sleep: async (ms) => {
				pauses.push(ms);
			},
		});
		expect(data.toString()).toBe("book");
		expect(pauses).toEqual([100, 200]);
	});

	it("gives up after three tries and reports cause and status", async () => {
		const fetchFn = vi.fn().mockRejectedValue(
			new TypeError("fetch failed", { cause: Object.assign(new Error("reset"), { code: "ECONNRESET" }) }),
		);
		await expect(fetchBook("https://x/a.epub", { fetchFn, ...quiet })).rejects.toThrow(
			/fetch failed; cause: ECONNRESET reset/,
		);
		expect(fetchFn).toHaveBeenCalledTimes(3);
	});

	it("does not repeat a 404", async () => {
		const fetchFn = vi.fn().mockResolvedValue(status(404));
		await expect(fetchBook("https://x/a.epub", { fetchFn, ...quiet })).rejects.toThrow(
			/^HTTP 404$/,
		);
		expect(fetchFn).toHaveBeenCalledTimes(1);
	});
});

describe("obtainBook", () => {
	const dir = mkdtempSync(join(tmpdir(), "books-"));
	const files = join(dir, "files");
	const from = join(dir, "from");
	mkdirSync(files);
	mkdirSync(from);
	const book = { id: "b-one", source: { url: "https://x/one.epub" } };

	it("takes a local file that matches the lock without fetching", async () => {
		writeFileSync(join(from, "b-one.epub"), "local");
		const fetchFn = vi.fn();
		const got = await obtainBook(book, {
			files,
			from,
			lock: { "b-one": { sha256: sha256(Buffer.from("local")) } },
			fetchFn,
			...quiet,
		});
		expect(got.origin).toBe("local");
		expect(fetchFn).not.toHaveBeenCalled();
	});

	it("ignores a local file the lock does not pin and fetches", async () => {
		const fetchFn = vi.fn().mockResolvedValue(ok("site"));
		const got = await obtainBook(book, {
			files,
			from,
			lock: { "b-one": { sha256: sha256(Buffer.from("site")) } },
			fetchFn,
			...quiet,
		});
		expect(got.origin).toBe("site");
		expect(got.data.toString()).toBe("site");
	});
});
