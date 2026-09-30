import createClient, { type Middleware } from "openapi-fetch";
import type { components, paths } from "./schema";

/**
 * The one place that talks to the API, typed from the server's own OpenAPI
 * document: a path, a body or a response that does not exist fails to compile.
 */

/** Where the API lives, on the origin that serves the app. */
export const API_BASE = "/api/v1";

/** The error every endpoint answers with when it refuses. */
export type ApiErrorBody = components["schemas"]["APIError"];

export class ApiError extends Error {
	readonly status: number;
	readonly code: string;
	readonly fields: Record<string, string>;
	readonly requestId?: string;

	constructor(status: number, body: ApiErrorBody) {
		super(body.message);
		this.name = "ApiError";
		this.status = status;
		this.code = body.code;
		this.fields = body.fields ?? {};
		this.requestId = body.requestId;
	}
}

// A proxy or gateway in front of the server may answer with something that is
// not the envelope. That is still a failure the caller has to see as one.
async function toApiError(response: Response): Promise<ApiError> {
	try {
		const body = (await response.clone().json()) as { error?: ApiErrorBody };
		if (body.error?.code && body.error.message) {
			return new ApiError(response.status, body.error);
		}
	} catch {
		// Not JSON; fall through to the generic error.
	}
	return new ApiError(response.status, {
		code: "unexpected_response",
		message: `The server answered ${response.status} without saying why.`,
	});
}

const throwOnError: Middleware = {
	async onResponse({ response }) {
		if (!response.ok) {
			throw await toApiError(response);
		}
		return response;
	},
};

export const api = createClient<paths>({
	// Absolute, because a Request cannot be built from a relative URL outside
	// a browser, which is where the tests run.
	baseUrl: new URL(API_BASE, window.location.origin).toString(),
	// The session cookie belongs to this origin; nothing is sent elsewhere.
	credentials: "same-origin",
	// Resolved per call rather than captured here, so a test can replace it.
	fetch: (request) => globalThis.fetch(request),
});
api.use(throwOnError);
