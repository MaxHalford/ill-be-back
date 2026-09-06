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
