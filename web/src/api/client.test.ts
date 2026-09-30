import { describe, expect, it, vi } from "vitest";
import { api, ApiError } from "./client";

function respond(
	status: number,
	body: unknown,
	contentType = "application/json",
) {
	return vi.spyOn(globalThis, "fetch").mockResolvedValue(
		new Response(typeof body === "string" ? body : JSON.stringify(body), {
			status,
			headers: { "Content-Type": contentType },
		}),
	);
}

describe("api client", () => {
	it("calls the API under its prefix and returns the typed body", async () => {
		const fetch = respond(200, { version: "1.2.3" });

		const { data } = await api.GET("/version");

		expect(data).toEqual({ version: "1.2.3" });
		const request = fetch.mock.calls[0]?.[0] as Request;
		expect(new URL(request.url).pathname).toBe("/api/v1/version");
		expect(request.credentials).toBe("same-origin");
	});

	it("throws the server's error envelope as an ApiError", async () => {
		respond(422, {
			error: {
				code: "validation_failed",
				message: "Some fields need attention.",
				fields: { title: "A title is required." },
				requestId: "abc123",
			},
		});

		const failure = await api.GET("/version").catch((e: unknown) => e);

		expect(failure).toBeInstanceOf(ApiError);
		expect(failure).toMatchObject({
			status: 422,
			code: "validation_failed",
			message: "Some fields need attention.",
			fields: { title: "A title is required." },
			requestId: "abc123",
		});
	});

	it("still fails clearly when the answer is not the envelope", async () => {
		respond(502, "<html>Bad Gateway</html>", "text/html");

		const failure = await api.GET("/version").catch((e: unknown) => e);

		expect(failure).toBeInstanceOf(ApiError);
		expect(failure).toMatchObject({
			status: 502,
			code: "unexpected_response",
			fields: {},
		});
	});
});
