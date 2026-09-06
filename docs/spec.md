# I'll Be Back, first release

Confirmed in the product interview on September 6, 2026.

Build a polished responsive web app for workers who want customers and colleagues
to know they are unavailable. One manual away/back button. No schedules, dates,
timezones, notification controls, payment system, or team administration.

- Connect one GitHub account, one Slack workspace, or either alone via OAuth.
- Away persists until manually cleared. Slack: custom status and palm emoji.
  GitHub: profile status, palm emoji, and limited availability.
- Back clears those statuses and GitHub limited availability. Do not restore old statuses.
- Editable per-app messages default to "Out of office", displayed with a palm emoji.
- Persist preferences and connections. Do not prompt for text on each toggle.
- Show each integration's update result. Retain successful changes on partial failure;
  offer retry/reconnection. Do not claim all services updated if some failed.
- Simple, down-to-earth visual design inspired by https://playaphone.com/: a white
  page with blue margins, system fonts, readable text, one primary button, and a plain
  list of apps. No pixel art, character imagery, or Terminator-themed copy. This replaces
  the initial artwork direction at the user's request. Keep explicit availability labels.
- GitHub and Slack ship first. Gmail vacation replies and Google Calendar absence
  events are the next milestone, before scheduling and notification controls.
- Host on Railway (the user corrected "Southworks"). The backend must be Go.

Implementation decisions: OAuth sign-in doubles as first connection. Provider tokens
stay encrypted server-side; secure server sessions and CSRF protect changes. A labeled
interactive preview does not call providers or imply actual account connections.

Confirmed test seam: the HTTP API, covering OAuth/account isolation, away/back,
per-app messages, and partial failures. Simulate external provider HTTP responses;
exercise real application routing, sessions, CSRF, encryption, and database storage.
