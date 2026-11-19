# Better Auth

Better Auth has no webhooks, so uncrm ships a small plugin that sends users from inside your app. Setup, the exact payload and failure behaviour are in [integrations/better-auth/README.md](../../integrations/better-auth/README.md).

In short: connect Better Auth in uncrm's Settings to get a URL and a `whsec_` secret, `npm install @uncrm/better-auth`, and add `uncrm({ url, secret })` to `plugins`.
