import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

export const sha256 = (data) => createHash("sha256").update(data).digest("hex");

/** What went wrong, including the cause: fetch only says "fetch failed" and keeps the reason (DNS, TLS, reset) in err.cause. */
export function describeError(err) {
	const parts = [err.message];
	for (let c = err.cause, depth = 0; c && depth < 3; c = c.cause, depth++) {
		parts.push(`cause: ${[c.code, c.message].filter(Boolean).join(" ")}`);
	}
	return parts.join("; ");
}

const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/** Downloads a URL, trying `attempts` times with a pause that doubles. A 4xx other than 408 and 429 is final. */
export async function fetchBook(
	url,
	{
		fetchFn = fetch,
		attempts = 3,
		pauseMs = 2000,
		sleep = wait,
		log = console.error,
	} = {},
) {
	let last;
	for (let n = 1; n <= attempts; n++) {
		try {
			const res = await fetchFn(url, {
				headers: { "user-agent": "GOtome-demo-sample-books" },
			});
			if (!res.ok) {
				const err = new Error(`HTTP ${res.status}`);
				err.status = res.status;
				throw err;
			}
			return Buffer.from(await res.arrayBuffer());
		} catch (err) {
			last = err;
			const final =
				err.status >= 400 &&
				err.status < 500 &&
				err.status !== 408 &&
				err.status !== 429;
			if (n === attempts || final) break;
			const pause = pauseMs * 2 ** (n - 1);
			log(
				`${url}: attempt ${n} of ${attempts}: ${describeError(err)}; again in ${pause} ms`,
			);
			await sleep(pause);
		}
	}
	throw new Error(describeError(last), { cause: last });
}

/**
 * The bytes of a book: the file already in `files`, else the one in `from`
 * (each only if the lock does not pin another hash), else the download.
 * Returns where they came from.
 */
export async function obtainBook(book, { files, from, lock, ...fetchOptions }) {
	const pinned = lock[book.id]?.sha256;
	const candidates = [join(files, `${book.id}.epub`)];
	if (from) candidates.push(join(from, `${book.id}.epub`));
	for (const path of candidates) {
		if (!existsSync(path)) continue;
		const data = readFileSync(path);
		if (!pinned || sha256(data) === pinned) {
			return { data, origin: path === candidates[0] ? "kept" : "local" };
		}
		(fetchOptions.log ?? console.error)(
			`${book.id}: ${path} does not match the lock file, ignored`,
		);
	}
	return { data: await fetchBook(book.source.url, fetchOptions), origin: "site" };
}
