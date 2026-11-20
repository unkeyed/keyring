# Better Auth

This page connects Better Auth's sign-up webhooks, which record your product's users as contacts. To sign in to Keyring itself with your Better Auth accounts, see [Sign in through Better Auth](../sign-in.md#sign-in-through-better-auth).

Better Auth has no webhooks, so Keyring ships a small plugin that sends users from inside your app. Setup, the exact payload and failure behaviour are in [integrations/better-auth/README.md](../../integrations/better-auth/README.md).

In short: connect Better Auth in Keyring's Settings to get a URL and a `whsec_` secret, `npm install @keyring/better-auth`, and add `keyring({ url, secret })` to `plugins`.
