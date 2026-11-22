import { defineConfig } from "astro/config";
import node from "@astrojs/node";

// Astro renders every page on request, so the middleware can set headers and
// add the analytics tag from the runtime environment. The Node adapter's
// standalone server serves it all; Unkey sends traffic to PORT 8080.
export default defineConfig({
  site: "https://kyring.dev",
  output: "server",
  adapter: node({ mode: "standalone" }),
  devToolbar: { enabled: false },
  // Emit every asset, fonts included, as a file instead of a data: URL, so
  // the Content-Security-Policy can stay at font-src 'self'.
  vite: { build: { assetsInlineLimit: 0 } },
});
