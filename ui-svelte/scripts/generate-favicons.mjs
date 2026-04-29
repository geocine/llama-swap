// Generate raster favicons + ICO from public/favicon.svg.
// Run via `npm run gen:favicons` whenever the SVG logo changes.

import { readFileSync, writeFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";
import pngToIco from "png-to-ico";

const __dirname = dirname(fileURLToPath(import.meta.url));
const publicDir = resolve(__dirname, "..", "public");
const svgPath = resolve(publicDir, "favicon.svg");

// Background colour for raster favicons (zinc-950). The SVG glyph is white
// on transparent — without a background it would be invisible on light browser
// chrome on iOS / Android home screens.
const BG = { r: 9, g: 9, b: 11, alpha: 1 };

async function rasterise(size, outName, { bg = BG, padding = 0 } = {}) {
  const inner = size - padding * 2;
  const glyph = await sharp(svgPath, { density: 384 })
    .resize(inner, inner, { fit: "contain", background: { r: 0, g: 0, b: 0, alpha: 0 } })
    .png()
    .toBuffer();

  const buf = await sharp({
    create: { width: size, height: size, channels: 4, background: bg },
  })
    .composite([{ input: glyph, top: padding, left: padding }])
    .png({ compressionLevel: 9 })
    .toBuffer();

  const outPath = resolve(publicDir, outName);
  writeFileSync(outPath, buf);
  console.log(`  wrote ${outName}  (${size}×${size}, ${buf.length} bytes)`);
  return outPath;
}

async function main() {
  console.log("Reading", svgPath);
  readFileSync(svgPath);

  console.log("Generating PNGs…");
  const png16 = await rasterise(16, "favicon-16x16.png");
  const png32 = await rasterise(32, "favicon-32x32.png");
  const png48 = await rasterise(48, "favicon-48x48.png");
  await rasterise(96, "favicon-96x96.png");
  // Apple touch icon is shown on iOS home screens — give it a bit of padding
  // so the glyph doesn't run to the edge of the rounded square.
  await rasterise(180, "apple-touch-icon.png", { padding: 18 });
  await rasterise(192, "web-app-manifest-192x192.png", { padding: 20 });
  await rasterise(512, "web-app-manifest-512x512.png", { padding: 56 });

  console.log("Generating multi-resolution ICO…");
  const ico = await pngToIco([png16, png32, png48]);
  const icoPath = resolve(publicDir, "favicon.ico");
  writeFileSync(icoPath, ico);
  console.log(`  wrote favicon.ico  (16+32+48, ${ico.length} bytes)`);

  console.log("Done.");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
