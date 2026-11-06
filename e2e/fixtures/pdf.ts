/**
 * A PDF of the pages given, each saying "Page n" and carrying filler, so a
 * test can have a file as large as it needs without one in the repository.
 * The filler is a comment in each page's content stream: it weighs, and
 * draws nothing. A page numbered in passages also says that text. The tag goes into the file as a comment, which makes each
 * test's file another file to the upload's duplicate check. An outline
 * becomes the file's bookmarks, each leading to its page.
 */
export function makePdf(
	pages: number,
	fillerPerPage = 0,
	tag = "",
	passages: Record<number, string> = {},
	outline: { title: string; page: number }[] = [],
): Buffer {
	const objects: string[] = [];
	const add = (body: string) => {
		objects.push(body);
		return objects.length;
	};
	const catalog = add("");
	const tree = add("");
	const font = add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>");
	// Ten pages to a branch, the branches at the front of the file, as a
	// real file's page tree is: a reader finds any page by reading the
	// front and that page, not every page before it.
	const branches = Array.from({ length: Math.ceil(pages / 10) }, () => add(""));
	const leaves: number[][] = branches.map(() => []);
	for (let n = 1; n <= pages; n++) {
		const branch = Math.floor((n - 1) / 10);
		const filler = fillerPerPage > 0 ? `%${"x".repeat(fillerPerPage)}\n` : "";
		const passage = passages[n] ? ` BT /F1 12 Tf 72 640 Td (${passages[n]}) Tj ET` : "";
		const content = `${filler}BT /F1 36 Tf 72 700 Td (Page ${n}) Tj ET${passage}`;
		const stream = add(`<< /Length ${content.length} >>\nstream\n${content}\nendstream`);
		leaves[branch]?.push(
			add(
				`<< /Type /Page /Parent ${branches[branch]} 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 ${font} 0 R >> >> /Contents ${stream} 0 R >>`,
			),
		);
	}
	const refs = (ids: number[]) => ids.map((id) => `${id} 0 R`).join(" ");
	const pageIds = leaves.flat();
	let outlines = "";
	if (outline.length > 0) {
		const root = add("");
		const items = outline.map(() => add(""));
		outline.forEach((entry, i) => {
			const links = [
				i > 0 ? `/Prev ${items[i - 1]} 0 R` : "",
				i < items.length - 1 ? `/Next ${items[i + 1]} 0 R` : "",
			].join(" ");
			objects[(items[i] ?? 0) - 1] =
				`<< /Title (${entry.title}) /Parent ${root} 0 R ${links} /Dest [${pageIds[entry.page - 1]} 0 R /Fit] >>`;
		});
		objects[root - 1] = `<< /Type /Outlines /First ${items[0]} 0 R /Last ${items.at(-1)} 0 R /Count ${items.length} >>`;
		outlines = ` /Outlines ${root} 0 R`;
	}
	objects[catalog - 1] = `<< /Type /Catalog /Pages ${tree} 0 R${outlines} >>`;
	objects[tree - 1] = `<< /Type /Pages /Kids [${refs(branches)}] /Count ${pages} >>`;
	branches.forEach((id, i) => {
		const kids = leaves[i] ?? [];
		objects[id - 1] = `<< /Type /Pages /Parent ${tree} 0 R /Kids [${refs(kids)}] /Count ${kids.length} >>`;
	});

	let out = `%PDF-1.4\n%${tag}\n`;
	const offsets: number[] = [];
	objects.forEach((body, i) => {
		offsets.push(Buffer.byteLength(out));
		out += `${i + 1} 0 obj\n${body}\nendobj\n`;
	});
	const xref = Buffer.byteLength(out);
	out += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
	for (const offset of offsets) {
		out += `${String(offset).padStart(10, "0")} 00000 n \n`;
	}
	out += `trailer\n<< /Size ${objects.length + 1} /Root ${catalog} 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
	return Buffer.from(out, "latin1");
}

/**
 * A sentence on every page, text enough for extraction not to take the
 * file for a scan: for makePdf's passages.
 */
export function proseOnEveryPage(pages: number): Record<number, string> {
	return Object.fromEntries(
		Array.from({ length: pages }, (_, i) => [
			i + 1,
			"The clerk counted the coins twice before the ledger was closed for the night.",
		]),
	);
}
