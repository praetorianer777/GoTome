import { API_BASE, ApiError, type ApiErrorBody } from "@/api/client";
import type { components } from "@/api/schema";

export type Uploaded = components["schemas"]["Uploaded"];

/** The formats the server reads, as the file picker offers them. */
export const UPLOAD_FORMATS = [
	".epub",
	".pdf",
	".mobi",
	".azw",
	".azw3",
	".m4b",
	".m4a",
	".mp3",
	".flac",
	".ogg",
	".opus",
];

/**
 * Sends one file into a managed library. This is the one request that does
 * not go through the typed client: fetch cannot say how much of a body has
 * been sent, and a person uploading an audiobook wants to know.
 */
export function uploadFile(
	libraryId: string,
	file: File,
	onProgress: (sent: number) => void,
): Promise<Uploaded> {
	return new Promise((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open(
			"POST",
			new URL(
				`${API_BASE}/libraries/${encodeURIComponent(libraryId)}/uploads`,
				window.location.origin,
			).toString(),
		);
		xhr.withCredentials = true;
		xhr.responseType = "text";
		xhr.upload.onprogress = (event) => {
			if (event.lengthComputable && event.total > 0) {
				onProgress(event.loaded / event.total);
			}
		};
		xhr.onload = () => {
			let body: unknown;
			try {
				body = JSON.parse(xhr.responseText);
			} catch {
				body = undefined;
			}
			if (xhr.status === 200 && body) {
				resolve(body as Uploaded);
				return;
			}
			const error = (body as { error?: ApiErrorBody } | undefined)?.error;
			reject(
				new ApiError(
					xhr.status,
					error?.code && error.message
						? error
						: {
								code: "unexpected_response",
								message: `The server answered ${xhr.status} without saying why.`,
							},
				),
			);
		};
		// What fetch rejects with when no answer came, so that it reads the same.
		xhr.onerror = () => reject(new TypeError("Failed to upload"));
		const form = new FormData();
		form.append("file", file, file.name);
		xhr.send(form);
	});
}
