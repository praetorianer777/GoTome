// Fetches the public-domain books of the demo (src/demo/library.json, each
// book's `source`) into demo/files, and the cover out of each EPUB into
// demo/covers. Every file is checked against demo/sample-books.lock.json;
// a book that has no entry yet is recorded there, so the first run pins the
// hashes and a later run, or a later download, must match them.
// `--from <dir>` reads <dir>/<id>.epub first, as a file already in demo/files
// is; only what is missing or does not match the lock is downloaded (three
// tries each, and a failure says its cause).
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { inflateRawSync } from "node:zlib";
import { obtainBook, sha256 } from "./sample-books-fetch.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const files = join(root, "demo/files");
const covers = join(root, "demo/covers");
const lockPath = join(root, "demo/sample-books.lock.json");
const library = JSON.parse(
	readFileSync(join(root, "src/demo/library.json"), "utf8"),
);
const lock = existsSync(lockPath)
	? JSON.parse(readFileSync(lockPath, "utf8"))
	: {};
mkdirSync(files, { recursive: true });
mkdirSync(covers, { recursive: true });

const fromAt = process.argv.indexOf("--from");
const from = fromAt > 0 ? process.argv[fromAt + 1] : undefined;
if (fromAt > 0 && !from) {
	console.error("--from needs a directory");
	process.exit(2);
}

// The entries of a zip, read from its central directory, so nothing but
// node is needed.
function* entries(zip) {
	let end = zip.length - 22;
	while (end >= 0 && zip.readUInt32LE(end) !== 0x06054b50) end--;
	if (end < 0) throw new Error("not a zip");
	let at = zip.readUInt32LE(end + 16);
	for (let n = zip.readUInt16LE(end + 10); n > 0; n--) {
		const method = zip.readUInt16LE(at + 10);
		const size = zip.readUInt32LE(at + 20);
		const nameLength = zip.readUInt16LE(at + 28);
		const extra = zip.readUInt16LE(at + 30) + zip.readUInt16LE(at + 32);
		const local = zip.readUInt32LE(at + 42);
		const name = zip.toString("utf8", at + 46, at + 46 + nameLength);
		yield {
			name,
			read() {
				const start =
					local +
					30 +
					zip.readUInt16LE(local + 26) +
					zip.readUInt16LE(local + 28);
				const raw = zip.subarray(start, start + size);
				return method === 0 ? raw : inflateRawSync(raw);
			},
		};
		at += 46 + nameLength + extra;
	}
}

function coverOf(id, epub) {
	const entry = [...entries(epub)].find((e) =>
		/(^|\/)cover[^/]*\.(jpe?g|png|webp)$/i.test(e.name),
	);
	if (!entry) return "no cover image in the EPUB";
	const image = entry.read();
	const ext = entry.name.split(".").pop().toLowerCase().replace("jpeg", "jpg");
	writeFileSync(join(covers, `${id}.${ext}`), image);
	return `cover ${id}.${ext} (${image.length} bytes)`;
}

let failed = 0;
let pinned = 0;
for (const book of library.books) {
	if (!book.source?.url) continue;
	const target = join(files, `${book.id}.epub`);
	let data;
	try {
		({ data } = await obtainBook(book, { files, from, lock }));
		const hash = sha256(data);
		const known = lock[book.id];
		if (known && known.sha256 !== hash) {
			throw new Error(`SHA-256 is ${hash}, the lock file pins ${known.sha256}`);
		}
		// Read as a zip before anything is kept: a site may answer a page instead.
		const cover = coverOf(book.id, data);
		if (!known) {
			lock[book.id] = {
				sha256: hash,
				size: data.length,
				url: book.source.url,
				licence: book.source.licence,
			};
			pinned++;
		}
		writeFileSync(target, data);
		console.log(`${book.id}: ${data.length} bytes, ${cover}`);
	} catch (err) {
		failed++;
		console.error(`${book.id}: ${book.source.url}: ${err.message}`);
	}
}
writeFileSync(lockPath, `${JSON.stringify(lock, null, 1)}\n`);
console.log(
	`${pinned} hashes pinned in demo/sample-books.lock.json; commit it.`,
);
if (failed > 0) {
	console.error(
		`${failed} books were not fetched; their URLs in src/demo/library.json need a look.`,
	);
	process.exit(1);
}
