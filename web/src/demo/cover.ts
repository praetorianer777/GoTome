function hash(text: string): number {
	let h = 7;
	for (const ch of text) h = (h * 31 + ch.codePointAt(0)!) >>> 0;
	return h;
}

function lines(text: string, width: number): string[] {
	const out: string[] = [];
	let line = "";
	for (const word of text.split(/\s+/)) {
		if (line && line.length + word.length + 1 > width) {
			out.push(line);
			line = word;
		} else {
			line = line ? `${line} ${word}` : word;
		}
	}
	if (line) out.push(line);
	return out.slice(0, 5);
}

const escape = (s: string) =>
	s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

/** A cover for a book that has none: its title and author on a colour its title decides. */
export function generatedCover(title: string, author: string): string {
	const hue = hash(title) % 360;
	const title_ = lines(title, 14)
		.map((l, i) => `<text x="150" y="${150 + i * 34}" text-anchor="middle" font-size="28" font-weight="700">${escape(l)}</text>`)
		.join("");
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 450"><rect width="300" height="450" fill="hsl(${hue} 35% 22%)"/><rect x="14" y="14" width="272" height="422" fill="none" stroke="hsl(${hue} 40% 62%)" stroke-width="2"/><g fill="hsl(${hue} 45% 90%)" font-family="Georgia, serif">${title_}<text x="150" y="400" text-anchor="middle" font-size="18" font-style="italic">${escape(author)}</text></g></svg>`;
	return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}
