# Google public launch

Project: `ill-be-back-507818` (number `602786024161`).
Owner/support/developer contact: `maxhalford25@gmail.com`.
App name: **I’ll Be Back**. Hosting: Railway; backend: Go.

Status: implementation prepared; Google access is for testing until verification
finishes. No security assessment has been purchased or booked. An owned public
domain and a real demonstration on supported accounts are still needed.

## OAuth configuration

Enable Gmail API and Google Calendar API. Create an **External** Google Auth
Platform app and a **Web application** client. Add the owner as a test user.
Use these redirect URLs for the current deployment:

- `https://ill-be-back-production.up.railway.app/auth/gmail/callback`
- `https://ill-be-back-production.up.railway.app/auth/calendar/callback`

Set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` through Railway's secret variables.
Do not commit credentials. Keep `GOOGLE_VERIFIED=false`; it displays the testing
notice. Google Cloud's testing audience, not this flag, limits authorization.
Testing grants for these permissions generally expire after seven days; reconnect
when prompted. This is not the public production configuration.

## Requested scopes and justification

| Scope | Purpose and why it is needed |
| --- | --- |
| `openid` | Stable Google subject for sign-in and account ownership. |
| `email` | Verified email displayed to distinguish multiple connected Google accounts. |
| `https://www.googleapis.com/auth/gmail.settings.basic` | Read/update the vacation responder. Read existing recipient restrictions, enable the user's reply on away, disable it on return. The vacation-settings methods do not offer a narrower vacation-only scope. No email messages are accessed. This is a restricted scope. |
| `https://www.googleapis.com/auth/calendar.events.owned` | Create, verify, update and delete an out-of-office event on the user's owned primary calendar. `calendar.app.created` only covers secondary calendars created by an app, which cannot host native out-of-office events. No event listing, other calendar browsing or invitation management. This is a sensitive scope. |

Each OAuth request includes identity/email plus only the service the user selected.
The server uses state, PKCE and session rotation, checks granted scopes and verifies
the Google subject using userinfo. It stores encrypted refresh credentials; access
tokens are transient. Gmail and Calendar sharing a Google subject share an IBB
sign-in identity. Different Google subjects are not merged by email.

## Before submitting

1. Choose a domain owned by Max and attach it to Railway. Verify domain ownership
   in Google Search Console with an account that owns/edits this Cloud project.
2. Set the canonical `APP_URL`, update all four providers' callbacks and the Google
   client redirects. Use the same public name throughout.
3. In Branding, set homepage to the canonical origin, privacy to `/privacy` and
   terms to `/terms`, and add the owned domain. These pages are publicly accessible
   without signing in. Confirm the privacy text matches actual operations and the
   deployed infrastructure's data retention/backup configuration.
4. In Data access, add the four scopes above. Supply the scope justifications and
   a demonstration video. Use test accounts without customer messages or meetings.
5. In Verification centre, submit branding and scope review. Complete the security
   assessment Google requests for server-side restricted-scope access, with an
   approved assessor. Confirm the quoted cost with Max before booking.
6. Resolve review feedback, publish to Production and set `GOOGLE_VERIFIED=true`
   only once the applicable reviews are approved. Verify public sign-in with a
   fresh external user, beyond the testing audience.

## Demonstration script

Record the browser and address bar, in English, showing the same OAuth client and
deployed app being submitted. Do not show secrets or personal inbox contents.

1. Open the public homepage and privacy page. Explain each integration's purpose.
2. Add Gmail using the picker. Show the complete Google consent flow and granted
   permissions. Add a second account to demonstrate distinct labels and messages.
3. Save a vacation message and press away. Show Gmail's vacation settings changing,
   with previous recipient restrictions preserved. Press back and show disabled.
4. Connect Calendar using a work test account that supports native out-of-office.
   Show its separate consent flow, choose a return time and switch away. Show the
   out-of-office event and that invitations are not declined. Retry and show there
   is still one event. Press back and show only that event removed.
5. Disconnect one Google connection, keeping other accounts intact. Show account
   deletion and the explanation that external statuses remain. Show Google's own
   access-revocation page as an additional control.

The automated suite simulates provider APIs and cannot replace this real-provider
demonstration. A personal Gmail address can test Gmail; it may not have Calendar's
native out-of-office feature. No silent fallback to a generic busy event is used.

## References

- [Gmail scopes and classifications](https://developers.google.com/workspace/gmail/api/auth/scopes)
- [Vacation update and allowed scope](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.settings/updateVacation)
- [Calendar status-event requirements](https://developers.google.com/workspace/calendar/api/guides/calendar-status)
- [Calendar authorization scopes](https://developers.google.com/workspace/calendar/api/auth)
- [Restricted scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/restricted-scope-verification)
- [Sensitive scope verification and domain requirements](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification)
- [Testing audience limits](https://support.google.com/cloud/answer/15549945?hl=en)
- [Google API Services User Data Policy](https://developers.google.com/terms/api-services-user-data-policy)
