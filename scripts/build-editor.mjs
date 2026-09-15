import { build } from "esbuild";
import { copyFileSync, mkdirSync } from "node:fs";
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
copyFileSync(
    resolve(root, "node_modules/chart.js/dist/chart.umd.min.js"),
    resolve(vendorDir, "chart.umd.min.js")
);
console.log("copied chart.js UMD -> static/vendor/chart.umd.min.js");