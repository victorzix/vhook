import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { describe, expect, it } from "vitest";

const SRC = path.resolve(__dirname, "..");

async function sourceFiles(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const found = await Promise.all(
    entries.map(async (entry) => {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) return sourceFiles(full);
      return /\.tsx?$/.test(entry.name) ? [full] : [];
    }),
  );
  return found.flat();
}

describe("isolamento dos mocks", () => {
  it("só os route handlers do BFF importam de mocks/", async () => {
    // É o que mantém a promessa do README: apagar a pasta e editar dois
    // arquivos. Sem este teste, um import solto transforma a remoção numa
    // caça ao erro de compilação.
    const offenders: string[] = [];

    for (const file of await sourceFiles(SRC)) {
      const relative = path.relative(SRC, file).replaceAll("\\", "/");
      if (relative.startsWith("mocks/")) continue;
      if (relative.startsWith("app/api/")) continue;

      const content = await readFile(file, "utf8");
      if (/from ["']@\/mocks|from ["'][./]+mocks\//.test(content)) {
        offenders.push(relative);
      }
    }

    expect(offenders, "estes arquivos importam de mocks/ e não deveriam").toEqual([]);
  });
});
