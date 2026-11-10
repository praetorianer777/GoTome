export function sha256(data: Uint8Array): string;
export function describeError(err: Error & { status?: number }): string;
export function fetchBook(
	url: string,
	options?: {
		fetchFn?: (url: string, init?: unknown) => Promise<Response>;
		attempts?: number;
		pauseMs?: number;
		sleep?: (ms: number) => Promise<void>;
		log?: (line: string) => void;
	},
): Promise<Buffer>;
export function obtainBook(
	book: { id: string; source: { url: string } },
	options: {
		files: string;
		from?: string;
		lock: Record<string, { sha256: string }>;
		fetchFn?: (url: string, init?: unknown) => Promise<Response>;
		attempts?: number;
		pauseMs?: number;
		sleep?: (ms: number) => Promise<void>;
		log?: (line: string) => void;
	},
): Promise<{ data: Buffer; origin: "kept" | "local" | "site" }>;
