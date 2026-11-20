// Steps launch.html through time at 30fps and saves each frame as a JPEG in
// frames/. Every frame is render(t), so the output is exact and repeatable
// however fast the machine is.
import { chromium } from "playwright";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const out = path.join(here, process.argv[2] ?? "frames");
const fps = 30;
fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });

const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1920, height: 1080 } });
await page.goto("file://" + path.join(here, "launch.html") + "#render", { waitUntil: "networkidle" });
await page.evaluate(() => document.fonts.ready);
const duration = await page.evaluate(() => window.DURATION);
const frames = Math.round(duration * fps);
for (let i = 0; i < frames; i++) {
  await page.evaluate((t) => window.render(t), i / fps);
  await page.screenshot({ path: path.join(out, String(i).padStart(5, "0") + ".jpg"), type: "jpeg", quality: 94 });
  if (i % 150 === 0) console.log(`frame ${i}/${frames}`);
}
await browser.close();
console.log(`${frames} frames in ${path.relative(process.cwd(), out)}`);
