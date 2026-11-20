# @keyring/better-auth

A [Better Auth](https://www.better-auth.com) plugin that sends every new, updated and deleted user to [Keyring](https://github.com/unkeyed/keyring).

Better Auth runs inside your app and has no webhooks of its own. This plugin hooks `databaseHooks.user.{create,update,delete}.after` and POSTs the user to Keyring, signed with [Standard Webhooks](https://www.standardwebhooks.com). It has no runtime dependencies.

## Setup

1. In Keyring, open **Settings > Providers > Better Auth** and click **Connect**. Copy the webhook URL and the secret it shows (the secret starts with `whsec_`).
2. Install the plugin:

   ```sh
   npm install @keyring/better-auth
   ```

3. Add it to your Better Auth config:

   ```ts
   import { betterAuth } from "better-auth";
   import { keyring } from "@keyring/better-auth";

   export const auth = betterAuth({
     // ...your existing options
     plugins: [
       keyring({
         url: "https://crm.example.com/webhooks/better-auth",
         secret: process.env.KEYRING_SECRET!,
       }),
     ],
   });
   ```

4. Set `KEYRING_SECRET` in your app's environment and redeploy.

Sign up a test user. Settings in Keyring shows "last delivery" within a few seconds.

## What it sends

Only these user fields leave your app: `id`, `email`, `emailVerified`, `name`, `image`, `createdAt` and `updatedAt`. Fields added by other plugins or by `user.additionalFields` are never sent.

```json
{
  "type": "user.created",
  "timestamp": "2026-11-19T12:00:00.000Z",
  "data": {
    "id": "Xk2v9aBcD",
    "email": "ada@example.com",
    "emailVerified": false,
    "name": "Ada Lovelace",
    "image": null,
    "createdAt": "2026-11-19T12:00:00.000Z",
    "updatedAt": "2026-11-19T12:00:00.000Z"
  }
}
```

`type` is `user.created`, `user.updated` or `user.deleted`. Headers are `webhook-id`, `webhook-timestamp` (Unix seconds) and `webhook-signature` (`v1,<base64 HMAC-SHA256>`).

## Failure behaviour

A delivery that fails (network error, a non-2xx answer, or no answer within 5 seconds) is logged with `console.error` and dropped. It never fails the sign-up. There is no retry, so a user created while Keyring is down reaches Keyring on their next profile update.

The request is awaited inside the hook. On serverless platforms, set Better Auth's `advanced.backgroundTasks.handler` (for example to `waitUntil`) and the plugin sends in the background instead, so sign-up does not wait for Keyring.

## Requirements

Better Auth 1.7 or later, on a runtime with global `fetch` and Web Crypto (Node 20+, Bun, Deno, Cloudflare Workers, Vercel Edge).
