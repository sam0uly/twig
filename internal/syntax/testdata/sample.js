import { readFile } from "node:fs/promises";

/* a block comment
   over two lines */

export async function load(path) {
  const text = await readFile(path, "utf8");
  const template = `a ${text}
over two lines`;
  if (text === null || text === undefined) {
    throw new Error('missing // not a comment');
  }
  // trailing comment with a "quote"
  return template;
}
