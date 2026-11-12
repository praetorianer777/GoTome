import { BASE_PATH } from "@/base";

// Installations on one origin under different paths share the browser's
// storage, so the scope, which the worker also names its caches by, is part
// of the names (sw.js builds the same ones).
const SCOPE = new URL(BASE_PATH, window.location.origin).href;

/** The files kept offline, by the address they are downloaded from. */
export const FILES_CACHE = `gotome-kept:${SCOPE}:files`;
/** The answers and covers the readers need for kept books. */
export const META_CACHE = `gotome-kept:${SCOPE}:meta`;
