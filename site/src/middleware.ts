import { defineMiddleware } from "astro:middleware";

// Every page and endpoint passes through here: it sets the security and
// caching headers, and adds the Databuddy analytics tag when
// DATABUDDY_CLIENT_ID is set. Files under /_astro/ and public/ are served by
// the adapter directly and get its own caching headers.

// The ID ends up in HTML, so only a UUID's characters are accepted. It is
// read at runtime from the Unkey environment, never from the repository.
// start.mjs refuses to start with anything else; this check keeps
// `astro dev` and `astro preview` safe too.
const raw = process.env.DATABUDDY_CLIENT_ID ?? "";
const clientId = /^[0-9a-f-]+$/i.test(raw) ? raw : "";

const analyticsTag = clientId
  ? `<script src="https://cdn.databuddy.cc/databuddy.v24.js" data-client-id="${clientId}" data-track-interactions="false" integrity="sha384-Y80DhLJtJQ3V+O23x9SiBGZ2WSME4bSRacQ1PMKEt13gsvMPDiD0sASSee31mZ6v" crossorigin="anonymous" async></script>`
  : "";
const scriptSrc = clientId ? " https://cdn.databuddy.cc" : "";
const beaconSrc = clientId ? " https://basket.databuddy.cc" : "";

const csp = [
  "default-src 'none'",
  `script-src 'self'${scriptSrc}`,
  `connect-src 'self'${beaconSrc}`,
  "style-src 'self' 'unsafe-inline'",
  "font-src 'self'",
  `img-src 'self' data:${beaconSrc}`,
  "base-uri 'none'",
  "form-action 'none'",
  "frame-ancestors 'none'",
].join("; ");

export const onRequest = defineMiddleware(async (_context, next) => {
  const response = await next();
  const type = response.headers.get("Content-Type") ?? "";
  const isHTML = type.startsWith("text/html");

  let out = response;
  if (isHTML && analyticsTag) {
    const body = (await response.text()).replace("</head>", `${analyticsTag}</head>`);
    out = new Response(body, { status: response.status, headers: response.headers });
  }

  const h = out.headers;
  h.set("Content-Security-Policy", csp);
  h.set("Strict-Transport-Security", "max-age=31536000; includeSubDomains");
  h.set("X-Content-Type-Options", "nosniff");
  h.set("Referrer-Policy", "strict-origin-when-cross-origin");
  h.set("X-Frame-Options", "DENY");
  if (!h.has("Cache-Control")) {
    // Pages and text revalidate so a deploy shows at once; text is small.
    h.set("Cache-Control", isHTML ? "no-cache" : "public, max-age=300");
  }
  return out;
});
