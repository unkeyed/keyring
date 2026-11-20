# Auth0

Keyring takes Auth0 users from an Event Streams webhook. Auth0 authenticates each delivery with a bearer token that Keyring generates for you. If your tenant has no Event Streams, a Post-Login Action does the same job.

## Setup with Event Streams (recommended)

1. In Keyring, open **Settings > Providers > Auth0** and click **Connect**. Copy the webhook URL (`https://<your-keyring-host>/webhooks/auth0`) and the token Keyring shows (it starts with `whsec_`).
2. In the [Auth0 Dashboard](https://manage.auth0.com), open **Event Streams** and click **Create Event Stream**, then choose **Webhook**.
3. Set **Webhook Endpoint** to the URL from step 1.
4. Set **Authorization Method** to **Bearer** and paste the token from step 1.
5. Select the events `user.created`, `user.updated` and `user.deleted`.
6. Save the stream and make sure it is enabled.
7. Use the stream's **Test** tab to send a test `user.created` event. Keyring's Settings page shows the delivery within a few seconds.

Event Streams covers every connection: database, passwordless, social and enterprise.

Auth0 tries each delivery 4 times over a few seconds and does not retry a 4xx answer. If Keyring was down, redeliver failed events from the stream's **Deliveries** tab.

## Setup with a Post-Login Action (fallback)

Use this when **Event Streams** is not in your Dashboard. It sends each user once, the first time they log in after you deploy it, so users who signed up earlier arrive as they log in. It does not send updates or deletes.

1. In Keyring, open **Settings > Providers > Auth0**, click **Connect**, and copy the webhook URL and token.
2. In the Auth0 Dashboard open **Actions > Library**, click **Create Action > Build from scratch**, name it `keyring`, choose the **Login / Post Login** trigger and the latest Node runtime, and click **Create**.
3. In the editor's **Secrets** panel (the key icon), add `KEYRING_URL` with the webhook URL and `KEYRING_TOKEN` with the token.
4. Replace the code with the code below and click **Deploy**.
5. Open **Actions > Triggers > post-login**, drag `keyring` from the right-hand panel into the flow, and click **Apply**.

```js
// Sends each user to keyring once, then marks them in app_metadata.
exports.onExecutePostLogin = async (event, api) => {
  const u = event.user;
  if (u.app_metadata.keyring_sent) return;
  const res = await fetch(event.secrets.KEYRING_URL, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${event.secrets.KEYRING_TOKEN}`,
    },
    body: JSON.stringify({
      id: `login_${u.user_id}`,
      source: "urn:auth0:action:post-login",
      specversion: "1.0",
      type: "user.created",
      time: u.created_at,
      data: {
        object: {
          user_id: u.user_id,
          email: u.email,
          email_verified: u.email_verified,
          name: u.name,
          given_name: u.given_name,
          family_name: u.family_name,
          picture: u.picture,
          created_at: u.created_at,
        },
      },
    }),
    signal: AbortSignal.timeout(3000),
  }).catch((err) => console.log(`keyring: ${err.message}`));
  if (res && res.ok) api.user.setAppMetadata("keyring_sent", true);
};
```

A failed or slow request never blocks the login. It is logged, and the user is sent again on their next login.

## What Keyring reads

| Keyring field | Auth0 field |
|---|---|
| external id | `data.object.user_id`, for example `auth0\|679f...` or `google-oauth2\|1082...` |
| email | `data.object.email` |
| email verified | `data.object.email_verified` |
| name | `data.object.name`, or `given_name` + `family_name` when `name` is just the email |
| avatar | `data.object.picture` |
| occurred at | the event's `time` |

Event types are accepted as `user.created` or `com.auth0.user.created` (and the same for updated and deleted). Other events, and users without an email, are accepted with a 200 and ignored.

## Verification

Keyring compares the `Authorization: Bearer <token>` header with the stored token in constant time. Auth0 does not sign Event Streams deliveries, so there is no timestamp check. Replays are harmless because Keyring writes every sign-up idempotently.
