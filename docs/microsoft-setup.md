# Microsoft Teams and Outlook setup

One Microsoft Entra app registration serves both integrations. Connecting either app
signs the person in to I’ll Be Back; the same Microsoft identity can then connect the
other service to that account. The integrations remain disabled until both Microsoft
environment variables are configured.

## Permissions in plain language

Microsoft has two permission types. **Delegated** permissions let the application act
for the person who signs in. **Application** permissions let software act without a
signed-in person and require administrator consent. I’ll Be Back uses delegated
permissions only.

| Permission | Requested for | Purpose |
| --- | --- | --- |
| `openid`, `profile`, `email` | Both | Identify the signed-in Microsoft account. |
| `User.Read` | Both | Read the account's own ID and display details. |
| `offline_access` | Both | Renew access when the person presses away/back later. |
| `Presence.ReadWrite` | Teams | Set and clear the person's Teams status message. |
| `MailboxSettings.ReadWrite` | Outlook | Read and update automatic reply settings. |

The service permissions cover more settings than I’ll Be Back uses. Outlook's
permission includes mailbox settings such as language and time zone, but does not
grant email-content reading or mail-sending permission. Outlook itself sends the
automatic replies. Teams presence permission is broader than just status text;
the integration only writes the status message.

The delegated permissions above do not inherently require admin consent. A company's
policy may nevertheless block user consent, require administrator approval, or allow
only verified publishers. Publisher verification can reduce installation friction,
but does not override a company's policy. Do not promise that every employee can
connect without involving IT.

Sources: [Microsoft Graph permissions reference](https://learn.microsoft.com/en-us/graph/permissions-reference),
[user and admin consent](https://learn.microsoft.com/en-us/entra/identity/enterprise-apps/user-admin-consent-overview).

## Register the application

1. Sign in to the [Microsoft Entra admin center](https://entra.microsoft.com/).
   You need an Entra directory in which your account can register applications;
   if your organization disables this, its administrator must create the registration
   or grant the appropriate role.
2. Open **Entra ID → App registrations → New registration**. Name it **I’ll Be Back**.
3. Select **Accounts in any organizational directory and personal Microsoft accounts**.
   This allows Microsoft 365 and Outlook.com. The Teams connection specifically uses
   the `organizations` authority, so personal accounts are excluded for Teams.
4. Under **Authentication**, add these **Web** redirect URIs, replacing `APP_URL`
   with the exact public origin:

   ```text
   APP_URL/auth/teams/callback
   APP_URL/auth/outlook/callback
   ```

   For the current Railway origin:

   ```text
   https://ill-be-back-production.up.railway.app/auth/teams/callback
   https://ill-be-back-production.up.railway.app/auth/outlook/callback
   ```

   Use a separate registration for local development, with callbacks under
   `http://localhost:8000`. Use the Web platform; implicit grants and public-client
   flows are unnecessary. The backend uses authorization code flow with PKCE.
5. Under **API permissions → Add a permission → Microsoft Graph → Delegated permissions**,
   add the permissions in the table above. `User.Read` may already be present.
   Do not select Application permissions or `Presence.ReadWrite.All`.
   Listing permissions permits an administrator to review them; the application's
   authorization requests ask only for the selected service, not `.default`.
6. Under **Certificates & secrets**, create a client secret. Put its **Value** in the
   server's secret configuration; the secret's ID is not the value. Record its expiry
   and replace it before it expires.
7. Set these server variables using the Application (client) ID from Overview:

   ```text
   MICROSOFT_CLIENT_ID=<application-client-id>
   MICROSOFT_CLIENT_SECRET=<client-secret-value>
   ```

   Keep the secret out of source control and browser code. Restart/redeploy the server
   after configuration. A tenant ID is not required by this implementation.
8. Set the homepage, privacy and terms URLs under the registration's branding settings
   to the app origin, `/privacy`, and `/terms`. Complete publisher verification if
   eligible before broad distribution; this code does not claim verified status.

Microsoft references: [register an application](https://learn.microsoft.com/en-us/entra/identity-platform/quickstart-register-app),
[authorization code flow](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow),
[consent experience](https://learn.microsoft.com/en-us/entra/identity-platform/application-consent-experience).

## Behavior and account requirements

- **Teams:** a work or school account with Teams access. Away sets `🌴 <saved message>`
  without an expiry. Back sets an empty status message. The presence indicator and
  notification preferences are not changed. The app currently limits saved Teams
  messages to 100 characters, consistently with its other short status fields.
- **Outlook:** a supported Microsoft 365 Exchange Online mailbox or Outlook.com
  mailbox. An arbitrary email account opened in the Outlook desktop application is
  not sufficient. Away sets `alwaysEnabled`, replaces both internal and external
  messages with HTML-escaped saved text, and preserves the current `externalAudience`.
  If external replies were disabled, they remain disabled. Old scheduled dates do
  not control replies in `alwaysEnabled` mode. Back patches only `status: disabled`.
- Both services keep separate messages and encrypted refresh credentials. The subject
  returned by Microsoft's UserInfo endpoint is the shared sign-in key, rather than
  the displayed email or a directory object ID alone. Removing the last connection
  for that subject removes its sign-in access to the local account.
- Microsoft refresh-token replacements are encrypted and saved before status writes.
  Revoked access and permission failures prompt reconnection; company policy failures
  mention possible administrator approval. Provider response bodies are not displayed.

API references: [Teams status messages](https://learn.microsoft.com/en-us/graph/api/presence-setstatusmessage?view=graph-rest-1.0),
[status message expiry](https://learn.microsoft.com/en-us/graph/api/resources/presencestatusmessage?view=graph-rest-1.0),
[Outlook automatic replies](https://learn.microsoft.com/en-us/graph/api/user-update-mailboxsettings?view=graph-rest-1.0),
[automatic reply settings](https://learn.microsoft.com/en-us/graph/api/resources/automaticrepliessetting?view=graph-rest-1.0),
[UserInfo](https://learn.microsoft.com/en-us/entra/identity-platform/userinfo).

## Validation before public enablement

The automated HTTP integration tests simulate Microsoft responses and exercise real
application sessions, persistence, account ownership, permission requests, token
rotation, HTML escaping, recipient preservation, away/back and partial failures.
They do not validate a real tenant's consent policies or change a live mailbox.

After configuring credentials, connect a test account for each service, save an away
message, and check away/back in Teams and Outlook themselves. Check that Outlook's
external audience stays unchanged and that both connections sign in to the same
I’ll Be Back account. Also test Outlook.com if personal accounts will be advertised.
Treat live enablement as pending until these checks succeed.
