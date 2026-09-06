# I'll Be Back, first release

Confirmed in the product interview on September 6, 2026.

Build a polished responsive web app for workers who want customers and colleagues
to know they are unavailable. One manual away/back button. No schedules, dates,
timezones, notification controls, payment system, or team administration.

- Connect multiple GitHub and Slack accounts via OAuth. Slack connections distinguish
  both workspace and user. Each identity belongs to only one I’ll Be Back account.
  A single “Add an app” button opens a modal to choose the provider.
  Add another account without disconnecting an existing one; reconnecting the same
  identity refreshes it without duplicates. Any linked identity can sign in.
- Away persists until manually cleared. Slack: custom status and palm emoji.
  GitHub: profile status, palm emoji, and limited availability.
- Back clears those statuses and GitHub limited availability. Do not restore old statuses.
- Editable per-account messages default to "Out of office", displayed with a palm emoji.
- Persist preferences and connections. Do not prompt for text on each toggle.
- Show each connected account's update result. Retain successful changes on partial failure;
  offer retry/reconnection. Do not claim all services updated if some failed.
- Simple, down-to-earth visual design inspired by https://playaphone.com/: a white
  page with blue margins, system fonts, readable text, one primary button, and a plain
  list of apps. No pixel art, character imagery, or Terminator-themed copy. This replaces
  the initial artwork direction at the user's request. Keep explicit availability labels.
- GitHub and Slack ship first. Gmail vacation replies and Google Calendar absence
  events are the next milestone, before scheduling and notification controls.
- Host on Railway (the user corrected "Southworks"). The backend must be Go.

- Remove preview mode, the footer tagline, and the explanatory “I’m back” sentence
  under the main button. Use the 🌴 emoji as the favicon.
- Preserve existing accounts, sessions, tokens, messages, and statuses during migration.

Implementation decisions: OAuth sign-in doubles as first connection. Provider tokens
stay encrypted server-side; secure server sessions and CSRF protect changes.

Confirmed test seam: the HTTP API, covering OAuth/account isolation, away/back,
per-account messages, and partial failures. Simulate external provider HTTP responses;
exercise real application routing, sessions, CSRF, encryption, and database storage.

## Google milestone

User requested Gmail and Calendar and accepted Google's verification process for
a public app. Support and verification contact: maxhalford25@gmail.com.

- Add Gmail and Google Calendar to the existing picker, including multiple Google
  accounts. Gmail and Calendar for the same Google subject share one sign-in identity.
- Gmail away enables a vacation reply using the saved text. Preserve existing
  contacts/domain recipient restrictions, replace previous HTML/text and expiry.
  Back disables the responder. Do not read mail or request mail-reading scopes.
- Calendar creates native out-of-office events on the primary calendar of supported
  work accounts. Implementation assumption, disclosed during work: ask a return date
  and time when Calendar is connected, because Google requires an end time.
  This is the sole exception to the first release's no-dates scope. No scheduler.
  Other providers still require manual clearing. Do not decline invitations.
- Retry must not duplicate events, and back must delete only the event created here.
  Persist event tracking before requests; do not claim expired Calendar absences
  remain active. Report unsupported accounts and provider failures honestly.
- Request only identity/email and the selected service's required OAuth scope;
  store refresh credentials encrypted, renew access and support reconnect.
- Publish accurate privacy/terms and a support contact. Permit account/data deletion
  even when external credentials are revoked, explaining external statuses remain.
- Prepare Google Cloud and verification materials. Public Google availability remains
  pending owned-domain branding verification, scope review and any required security
  assessment. No claim of Google approval and no paid assessment booking.
