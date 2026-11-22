// Starts the site's server. It checks DATABUDDY_CLIENT_ID first, so a value
// that cannot safely go into HTML stops the container before it serves
// anything, then hands over to Astro's standalone server.
const id = process.env.DATABUDDY_CLIENT_ID ?? "";
if (id && /[^0-9a-f-]/i.test(id)) {
  console.error("analytics: DATABUDDY_CLIENT_ID must be a UUID");
  process.exit(1);
}
console.log(id ? "analytics: Databuddy tag on" : "analytics: DATABUDDY_CLIENT_ID is unset, serving without the tag");
await import("./dist/server/entry.mjs");
