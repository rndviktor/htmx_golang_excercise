// Tree-shaken Chart.js entry point: only the line-chart subsystems used by
// monitoring.js are imported and registered, so the bundled vendor file
// ships a fraction of the full UMD build. Built by scripts/build-editor.mjs.
import {
    Chart,
    LineController,
    LineElement,
    PointElement,
    LinearScale,
    CategoryScale,
    Legend,
    Tooltip,
    Filler,
} from "chart.js";

Chart.register(
    LineController,
    LineElement,
    PointElement,
    LinearScale,
    CategoryScale,
    Legend,
    Tooltip,
    Filler,
);

// Expose for classic scripts (monitoring.js uses the bare global `Chart`,
// mirroring how codemirror.bundle.js exposes window.SqlEditor).
window.Chart = Chart;

export { Chart };