import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const supportDirectory = dirname(fileURLToPath(import.meta.url));
export const e2eDirectory = resolve(supportDirectory, "..");
export const repositoryRoot = resolve(e2eDirectory, "..");
export const binaryPath = resolve(e2eDirectory, ".cache", process.platform === "win32" ? "change-saga.exe" : "change-saga");

export default function globalSetup(): void {
  mkdirSync(dirname(binaryPath), { recursive: true });
  execFileSync("go", ["build", "-o", binaryPath, "./cmd/change-saga"], {
    cwd: repositoryRoot,
    stdio: "inherit"
  });
}
