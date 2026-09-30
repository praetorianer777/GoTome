import { QueryClient } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { createElement } from "react";
import { vi } from "vitest";
import { App } from "@/app";
import type { CurrentUser } from "@/auth/session";
import { makeRouter } from "@/router";

const PERMISSIONS = {
	reader: ["library:read", "personal:manage"],
	editor: [
		"library:read",
		"personal:manage",
		"books:upload",
		"metadata:edit",
		"index:rebuild",
	],
	admin: [
		"library:read",
		"personal:manage",
		"books:upload",
		"metadata:edit",
		"index:rebuild",
		"users:manage",
		"settings:manage",
		"storage:manage",
	],
} as const;

export function user(
	username: string,
	role: keyof typeof PERMISSIONS = "admin",
): CurrentUser {
	return {
		id: `id-${username}`,
		username,
		role,
		permissions: [...PERMISSIONS[role]],
	};
}

interface Account {
	password: string;
	user: CurrentUser;
}

/**
 * A stand-in for the server's auth endpoints with the same rules: setup works
 * once, login needs the right password, /auth/me needs a session.
 */
export class FakeServer {
	accounts = new Map<string, Account>();
	session: CurrentUser | null = null;
	requests: { method: string; path: string; body: unknown }[] = [];
	/** Set to make every request fail as if the server were unreachable. */
	down = false;

	withAccount(
		username: string,
		password: string,
		role: keyof typeof PERMISSIONS = "admin",
	): this {
		this.accounts.set(username.toLowerCase(), {
			password,
			user: user(username, role),
		});
		return this;
	}

	signedInAs(username: string): this {
		this.session = this.accounts.get(username.toLowerCase())?.user ?? null;
		return this;
	}

	install(): this {
		vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
			if (this.down) {
				throw new TypeError("Failed to fetch");
			}
			const request = input as Request;
			const path = new URL(request.url).pathname.replace(/^\/api\/v1/, "");
			const body: unknown =
				request.method === "GET"
					? undefined
					: await request.json().catch(() => undefined);
			this.requests.push({ method: request.method, path, body });
			return this.answer(
				request.method,
				path,
				body as Record<string, string> | undefined,
			);
		});
		return this;
	}

	private answer(
		method: string,
		path: string,
		body: Record<string, string> = {},
	): Response {
		const refuse = (
			status: number,
			code: string,
			message: string,
			fields?: Record<string, string>,
		) =>
			Response.json(
				{ error: { code, message, fields, requestId: "req-1" } },
				{ status },
			);

		switch (`${method} ${path}`) {
			case "GET /setup":
				return Response.json({ needed: this.accounts.size === 0 });
			case "POST /setup": {
				if (this.accounts.size > 0) {
					return refuse(
						409,
						"conflict",
						"GOtome is already set up. Sign in instead.",
					);
				}
				if ((body.password ?? "").length < 8) {
					return refuse(
						422,
						"validation_failed",
						"Some fields need attention.",
						{
							password: "A password has at least 8 characters.",
						},
					);
				}
				this.withAccount(body.username ?? "", body.password ?? "").signedInAs(
					body.username ?? "",
				);
				return Response.json(this.session, { status: 201 });
			}
			case "POST /auth/login": {
				const account = this.accounts.get((body.username ?? "").toLowerCase());
				if (!account || account.password !== body.password) {
					return refuse(
						401,
						"unauthorized",
						"The user name or the password is wrong.",
					);
				}
				this.session = account.user;
				return Response.json(account.user);
			}
			case "POST /auth/logout":
				this.session = null;
				return new Response(null, { status: 204 });
			case "GET /auth/me":
				return this.session
					? Response.json(this.session)
					: refuse(401, "unauthorized", "Sign in to continue.");
			default:
				return refuse(404, "not_found", "There is nothing at this address.");
		}
	}
}

/** Renders the whole app at an address, against the fake server. */
export function renderApp(path: string) {
	// No retries: a test that expects a failure should not wait for three.
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	const history = createMemoryHistory({ initialEntries: [path] });
	const router = makeRouter(queryClient, history);
	return { router, ...render(createElement(App, { queryClient, router })) };
}
