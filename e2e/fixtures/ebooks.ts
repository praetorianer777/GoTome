/**
 * EPUB and MOBI files made for the reader's browser tests: chapters of
 * numbered paragraphs, long enough to run over several pages. The tag goes
 * into each file, which makes every test's file another file to the
 * upload's duplicate check.
 */

const CRC_TABLE = Array.from({ length: 256 }, (_, n) => {
	let c = n;
	for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
	return c >>> 0;
});

function crc32(data: Buffer): number {
	let c = 0xffffffff;
	for (const byte of data) c = (CRC_TABLE[(c ^ byte) & 0xff] ?? 0) ^ (c >>> 8);
	return (c ^ 0xffffffff) >>> 0;
}

/** A zip of the files given, in that order, stored without compression. */
function zip(files: [string, string][]): Buffer {
	const local: Buffer[] = [];
	const central: Buffer[] = [];
	let offset = 0;
	for (const [name, text] of files) {
		const data = Buffer.from(text, "utf8");
		const fileName = Buffer.from(name, "utf8");
		const crc = crc32(data);
		const header = Buffer.alloc(30);
		header.writeUInt32LE(0x04034b50, 0);
		header.writeUInt16LE(10, 4);
		header.writeUInt32LE(crc, 14);
		header.writeUInt32LE(data.length, 18);
		header.writeUInt32LE(data.length, 22);
		header.writeUInt16LE(fileName.length, 26);
		local.push(header, fileName, data);
		const entry = Buffer.alloc(46);
		entry.writeUInt32LE(0x02014b50, 0);
		entry.writeUInt16LE(20, 4);
		entry.writeUInt16LE(10, 6);
		entry.writeUInt32LE(crc, 16);
		entry.writeUInt32LE(data.length, 20);
		entry.writeUInt32LE(data.length, 24);
		entry.writeUInt16LE(fileName.length, 28);
		entry.writeUInt32LE(offset, 42);
		central.push(entry, fileName);
		offset += header.length + fileName.length + data.length;
	}
	const directory = Buffer.concat(central);
	const end = Buffer.alloc(22);
	end.writeUInt32LE(0x06054b50, 0);
	end.writeUInt16LE(files.length, 8);
	end.writeUInt16LE(files.length, 10);
	end.writeUInt32LE(directory.length, 12);
	end.writeUInt32LE(offset, 16);
	return Buffer.concat([...local, directory, end]);
}

function paragraphs(chapter: number, count: number, from = 1): string {
	return Array.from(
		{ length: count },
		(_, i) =>
			`<p>Chapter ${chapter}, paragraph ${from + i}. It is a truth universally acknowledged that a reader in want of a test must be in possession of many words, and these are some of them.</p>`,
	).join("\n");
}

/**
 * An EPUB 3 of the chapters, each with a heading and a table of contents
 * entry. With hostile, the first chapter carries a script, an event
 * handler and an image from another host, none of which may do anything.
 * A chapter numbered in passages has that text as a paragraph halfway
 * through.
 */
export function makeEpub(
	title: string,
	chapters: number,
	tag: string,
	hostile = false,
	passages: Record<number, string> = {},
): Buffer {
	const ids = Array.from({ length: chapters }, (_, i) => i + 1);
	const attack = hostile
		? `<script>parent.document.body.dataset.bookScript = "ran"; window.bookScript = "ran";</script>
<img src="data:," alt="" onerror="parent.document.body.dataset.bookHandler = 'ran'"/>
<img src="https://example.com/tracker.png" alt=""/>`
		: "";
	const chapter = (n: number) => `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>Chapter ${n}</title></head>
<body>
<h1>Chapter ${n}</h1>
${n === 1 ? attack : ""}
${paragraphs(n, 20)}
${passages[n] ? `<p>${passages[n]}</p>` : ""}
${paragraphs(n, 20, 21)}
</body>
</html>`;
	const nav = `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>Contents</title></head>
<body><nav epub:type="toc"><ol>
${ids.map((n) => `<li><a href="c${n}.xhtml">Chapter ${n}</a></li>`).join("\n")}
</ol></nav></body>
</html>`;
	const opf = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:identifier id="id">urn:test:${tag}</dc:identifier>
<dc:title>${title}</dc:title>
<dc:language>en</dc:language>
<meta property="dcterms:modified">2026-01-01T00:00:00Z</meta>
</metadata>
<manifest>
<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
${ids.map((n) => `<item id="c${n}" href="c${n}.xhtml" media-type="application/xhtml+xml"${n === 1 && hostile ? ' properties="scripted remote-resources"' : ""}/>`).join("\n")}
</manifest>
<spine>${ids.map((n) => `<itemref idref="c${n}"/>`).join("")}</spine>
</package>`;
	return zip([
		["mimetype", "application/epub+zip"],
		[
			"META-INF/container.xml",
			`<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		],
		["OEBPS/content.opf", opf],
		["OEBPS/nav.xhtml", nav],
		...ids.map((n): [string, string] => [`OEBPS/c${n}.xhtml`, chapter(n)]),
	]);
}

const NO_INDEX = 0xffffffff;

/**
 * A Mobipocket file of the chapters, uncompressed, a page break between
 * them, laid out as the backend's own MOBI tests write one.
 */
export function makeMobi(title: string, chapters: number, tag: string): Buffer {
	const markup = Buffer.from(
		`<html><head><guide></guide></head><body><p>${tag}</p>${Array.from(
			{ length: chapters },
			(_, i) => `<h1>Chapter ${i + 1}</h1>${paragraphs(i + 1, 40)}`,
		).join("<mbp:pagebreak/>")}</body></html>`,
		"utf8",
	);
	const recordSize = 4096;
	const records: Buffer[] = [Buffer.alloc(0)];
	for (let start = 0; start < markup.length; start += recordSize) {
		records.push(markup.subarray(start, start + recordSize));
	}
	const textRecords = records.length - 1;
	records.push(Buffer.from([0xe9, 0x8e, 0x0d, 0x0a]));

	const mobiLength = 0xe8;
	let rec0 = Buffer.alloc(16 + mobiLength);
	rec0.writeUInt16BE(1, 0);
	rec0.writeUInt32BE(markup.length, 4);
	rec0.writeUInt16BE(textRecords, 8);
	rec0.writeUInt16BE(recordSize, 10);
	rec0.write("MOBI", 16, "latin1");
	rec0.writeUInt32BE(mobiLength, 0x14);
	rec0.writeUInt32BE(2, 0x18);
	rec0.writeUInt32BE(65001, 0x1c);
	rec0.writeUInt32BE(0x1234, 0x20);
	rec0.writeUInt32BE(6, 0x24);
	for (let off = 0x28; off < 0x50; off += 4) rec0.writeUInt32BE(NO_INDEX, off);
	rec0.writeUInt32BE(textRecords + 1, 0x50);
	rec0.writeUInt32BE(9, 0x5c);
	rec0.writeUInt32BE(6, 0x68);
	rec0.writeUInt32BE(NO_INDEX, 0x6c);
	rec0.writeUInt32BE(NO_INDEX, 0x70);
	rec0.writeUInt32BE(0x40, 0x80);
	rec0.writeUInt16BE(1, 0xc0);
	rec0.writeUInt16BE(textRecords, 0xc2);
	rec0.writeUInt32BE(NO_INDEX, 0xe4);

	const exth = Buffer.alloc(12);
	exth.write("EXTH", 0, "latin1");
	exth.writeUInt32BE(12, 4);
	const name = Buffer.from(title, "utf8");
	rec0 = Buffer.concat([rec0, exth]);
	rec0.writeUInt32BE(rec0.length, 0x54);
	rec0.writeUInt32BE(name.length, 0x58);
	rec0 = Buffer.concat([rec0, name, Buffer.alloc(2)]);
	rec0 = Buffer.concat([rec0, Buffer.alloc((4 - (rec0.length % 4)) % 4)]);
	records[0] = rec0;

	const header = Buffer.alloc(78);
	header.write(title.slice(0, 31), 0, "latin1");
	header.write("BOOKMOBI", 60, "latin1");
	header.writeUInt16BE(records.length, 76);
	const list = Buffer.alloc(8 * records.length + 2);
	let offset = 78 + list.length;
	records.forEach((r, i) => {
		list.writeUInt32BE(offset, 8 * i);
		list.writeUInt32BE(i & 0xffffff, 8 * i + 4);
		offset += r.length;
	});
	return Buffer.concat([header, list, ...records]);
}
