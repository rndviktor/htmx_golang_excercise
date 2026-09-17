import { build } from "esbuild";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

await build({
    entryPoints: [resolve(root, "static/js/sql-editor.js")],
    bundle: true,
    format: "esm",
    minify: true,
    target: "es2022",
    outfile: resolve(root, "static/codemirror.bundle.js"),
    sourcemap: false,
    logLevel: "info",
});

const vendorDir = resolve(root, "static/vendor");
mkdirSync(vendorDir, { recursive: true });

await build({
    entryPoints: [resolve(root, "static/js/chart-entry.js")],
    bundle: true,
    format: "esm",
    minify: true,
    target: "es2022",
    outfile: resolve(vendorDir, "chart.bundle.js"),
    sourcemap: false,
    logLevel: "info",
});
console.log("built tree-shaken chart.js -> static/vendor/chart.bundle.js");