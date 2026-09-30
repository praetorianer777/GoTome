import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// make writes what this checkout's stack publishes to .cache/stack.env. The
// suite reads the address from there, so it always talks to the stack of the
// checkout it was started in and never to another worktree's.
function stackEnv(): Record<string, string> {
	const path = fileURLToPath(
		new URL("../../.cache/stack.env", import.meta.url),
	);
	let text: string;
	try {
		text = readFileSync(path, "utf8");
	} catch {
		throw new Error(
			`${path} is missing. Start the stack with make up or make stack-up.`,
		);
	}
	return Object.fromEntries(
		text
			.split("\n")
			.filter((line) => line.includes("="))
			.map((line) => [
				line.slice(0, line.indexOf("=")),
				line.slice(line.indexOf("=") + 1),
			]),
	);
}

export const BASE_URL = process.env.GOTOME_URL ?? stackEnv().GOTOME_URL ?? "";

/**
 * The account the suite sets a fresh stack up with. The stack is a throwaway:
 * these are test values and exist nowhere else.
 */
export const ADMIN = {
	username: "e2e-admin",
	password: "e2e-throwaway-stack-only",
};
