import { useEffect, useRef, useState } from "react";
import {
  Check,
  ExternalLink,
  LoaderCircle,
  LogOut,
  Plus,
  RefreshCw,
  ShieldCheck,
  Unplug,
  X,
} from "lucide-react";
import { getState, request } from "./api";
import { AppIcon } from "./icons";
import type { AppState, Availability, Connection, Provider } from "./types";

const names: Record<Provider, string> = {
  github: "GitHub",
  slack: "Slack",
  gmail: "Gmail",
  calendar: "Google Calendar",
};
const limits: Record<Provider, number> = {
  github: 80,
  slack: 100,
  gmail: 1000,
  calendar: 100,
};
const descriptions: Record<Provider, string> = {
  github: "Profile status and availability",
  slack: "Profile status in your workspace",
  gmail: "Vacation replies using your saved message",
  calendar: "Out-of-office events · supported work accounts only",
};
function localDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16);
}
const empty: AppState = {
  authenticated: false,
  name: "",
  desiredStatus: "available",
  returnAt: "",
  connections: [],
  providers: { github: false, slack: false, gmail: false, calendar: false },
  csrfToken: "",
};
export default function App() {
  const [state, setState] = useState<AppState>(empty);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [modal, setModal] = useState<
    "settings" | "connect" | "return" | "delete" | null
  >(null);
  const [returnAt, setReturnAt] = useState("");
  const [messages, setMessages] = useState<Record<string, string>>({});
  const [disconnecting, setDisconnecting] = useState<string | null>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const [now, setNow] = useState(Date.now);
  const connections = state.connections.map((c) =>
    c.provider === "calendar" &&
    c.status === "away" &&
    new Date(c.awayUntil || "").getTime() <= now
      ? {
          ...c,
          status: "unknown" as const,
          error:
            "Your Calendar absence ended. Choose a new return time or switch back.",
        }
      : c,
  );
  useEffect(() => {
    const deadlines = state.connections
      .filter((c) => c.provider === "calendar" && c.status === "away")
      .map((c) => new Date(c.awayUntil || "").getTime())
      .filter((end) => end > now);
    if (!deadlines.length) return;
    const timer = window.setTimeout(
      () => setNow(Date.now()),
      Math.min(
        2_147_483_647,
        Math.max(0, Math.min(...deadlines) - Date.now()) + 50,
      ),
    );
    return () => window.clearTimeout(timer);
  }, [state.connections, now]);
  const desired = state.desiredStatus;
  const away = desired === "away";
  const succeeded = connections.filter(
    (c) => c.status === desired && !c.error,
  ).length;
  const failed = connections.some((c) => c.error);
  const synced = connections.length > 0 && succeeded === connections.length;

  useEffect(() => {
    getState()
      .then(setState)
      .catch((e) => setNotice(e.message))
      .finally(() => setLoading(false));
    const params = new URLSearchParams(window.location.search);
    if (params.has("error")) {
      const errors: Record<string, string> = {
        cancelled:
          "Connection cancelled. You can try again whenever you’re ready.",
        oauth: "We couldn’t connect that account. Please try again.",
        state: "That connection link expired. Please try connecting again.",
        wrong_account:
          "That is a different account. Please reconnect using the original account.",
        linked: "That account is already linked to another sign-in.",
        unavailable:
          "This connection isn’t available yet. Please try again later.",
        busy: "A status update is in progress. Please try connecting again in a moment.",
      };
      setNotice(
        errors[params.get("error") || ""] ||
          "Unable to connect. Please try again.",
      );
      window.history.replaceState(null, "", window.location.pathname);
    }
  }, []);

  useEffect(() => {
    if (modal) dialog.current?.showModal();
    else dialog.current?.close();
  }, [modal]);

  // A refresh on returning to this tab picks up changes made from another device.
  useEffect(() => {
    const refresh = () => {
      if (!document.hidden && !busy)
        getState()
          .then(setState)
          .catch(() => {});
    };
    document.addEventListener("visibilitychange", refresh);
    return () => document.removeEventListener("visibilitychange", refresh);
  }, [busy]);

  function openSettings() {
    setMessages(Object.fromEntries(connections.map((c) => [c.id, c.message])));
    setDisconnecting(null);
    setModal("settings");
  }

  function startUpdate(target: Availability) {
    if (
      target === "away" &&
      connections.some((c) => c.provider === "calendar")
    ) {
      setReturnAt(
        state.returnAt && new Date(state.returnAt).getTime() > Date.now()
          ? localDateTime(state.returnAt)
          : "",
      );
      setModal("return");
      return;
    }
    void update(target);
  }

  async function update(target: Availability, until = "") {
    if (!state.authenticated) {
      setModal("connect");
      return;
    }
    setBusy(true);
    setNotice("");
    try {
      setState(
        await request<AppState>("status", state.csrfToken, {
          status: target,
          returnAt: until,
        }),
      );
      setModal(null);
    } catch (e) {
      setNotice((e as Error).message);
      setState((current) => ({
        ...current,
        desiredStatus: target,
        connections: current.connections.map((c) => ({
          ...c,
          status: "unknown",
          error: "Update not confirmed. Check your connection, then retry.",
        })),
      }));
      getState()
        .then(setState)
        .catch(() => {});
    } finally {
      setBusy(false);
    }
  }

  async function saveMessages(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setNotice("");
    try {
      setState(
        await request<AppState>("messages", state.csrfToken, { messages }),
      );
      setModal(null);
      setNotice(
        "Messages saved. They’ll be used the next time you switch away.",
      );
    } catch (e) {
      setNotice((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function disconnect(connection: Connection) {
    setBusy(true);
    setNotice("");
    try {
      setState(
        await request<AppState>(
          `connections/${encodeURIComponent(connection.id)}`,
          state.csrfToken,
          {},
          "DELETE",
        ),
      );
      setDisconnecting(null);
      setModal(null);
    } catch (e) {
      setNotice((e as Error).message);
      setState((current) => ({
        ...current,
        connections: current.connections.map((c) =>
          c.id === connection.id
            ? {
                ...c,
                status: "unknown",
                error:
                  "Disconnect not confirmed. Check your connection, then retry.",
              }
            : c,
        ),
      }));
      getState()
        .then(setState)
        .catch(() => {});
    } finally {
      setBusy(false);
    }
  }

  async function logout() {
    setBusy(true);
    try {
      setState(await request<AppState>("logout", state.csrfToken, {}));
      setModal(null);
    } catch (e) {
      setNotice((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function deleteAccount() {
    setBusy(true);
    setNotice("");
    try {
      setState(
        await request<AppState>("account", state.csrfToken, {}, "DELETE"),
      );
      setModal(null);
      setNotice("Your account and stored connections have been deleted.");
    } catch (e) {
      setNotice((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const connectionButtons = (
    <div className="connect-options">
      {(["slack", "github", "gmail", "calendar"] as Provider[]).map(
        (provider) => (
          <a
            key={provider}
            className={`connect-option ${!state.providers[provider] || busy ? "disabled" : ""}`}
            href={
              state.providers[provider] && !busy
                ? `/auth/${provider}`
                : undefined
            }
            aria-disabled={!state.providers[provider] || busy}
          >
            <AppIcon app={provider} />
            <span>
              {names[provider]}
              <small>{descriptions[provider]}</small>
              {!state.providers[provider] && <small>Not available yet</small>}
            </span>
          </a>
        ),
      )}
    </div>
  );

  return (
    <div className="page">
      <header>
        <h1>
          <a href="/">I’ll Be Back</a>
        </h1>
        <button
          className="text-button"
          onClick={() =>
            state.authenticated ? openSettings() : setModal("connect")
          }
        >
          {state.authenticated ? "Account & settings" : "Sign in"}
        </button>
      </header>
      <main>
        <p className="introduction">
          Let people know when you’re away. Set your out-of-office status in all
          your apps with one button.
        </p>

        {notice && (
          <div className="notice" role="status">
            <span>{notice}</span>
            <button aria-label="Dismiss message" onClick={() => setNotice("")}>
              <X size={18} />
            </button>
          </div>
        )}

        <section className="availability" aria-label="Availability controls">
          <div className="availability-heading">
            <span
              className={`status-dot ${away ? "away" : ""} ${connections.length && !synced ? "unconfirmed" : ""}`}
              aria-hidden="true"
            />
            <h2>
              {connections.length && !synced
                ? failed
                  ? "Some apps need attention."
                  : "Ready to update your accounts."
                : away
                  ? "You’re away."
                  : connections.length
                    ? "You’re available."
                    : "Ready when you are."}
            </h2>
          </div>
          <p>
            {!connections.length
              ? "Connect an app below to get started."
              : !synced
                ? "Check the results below. You can retry any updates that haven’t been confirmed."
                : away
                  ? connections.some((c) => c.provider === "calendar")
                    ? "Calendar ends at your return time. Use I’m back to clear your other apps."
                    : "Your away statuses will stay on until you come back."
                  : "Taking some time off? Let your apps know."}
          </p>
          <button
            className="primary-button"
            disabled={loading || busy}
            onClick={() => startUpdate(away ? "available" : "away")}
          >
            {busy && <LoaderCircle className="spin" size={18} />}
            {loading
              ? "Loading…"
              : busy
                ? "Updating…"
                : away
                  ? "I’m back"
                  : "I’m away"}
          </button>
          {connections.length > 0 && (
            <div className="sync-result" aria-live="polite">
              <span>
                {succeeded} of {connections.length} accounts{" "}
                {away ? "set to away" : "cleared"}.
              </span>
              {!synced && (
                <button
                  className="text-button"
                  disabled={busy}
                  onClick={() => startUpdate(desired)}
                >
                  {failed ? "Retry updates" : "Update apps"}{" "}
                  <RefreshCw size={13} />
                </button>
              )}
            </div>
          )}
        </section>

        <section className="apps-section" aria-labelledby="apps-heading">
          <div className="section-heading">
            <h2 id="apps-heading">Your apps</h2>
            <button className="text-button" onClick={openSettings}>
              Edit away messages
            </button>
          </div>
          <div className="app-list">
            {connections.map((c) => (
              <article className="app-row" key={c.id}>
                <div className="app-row-heading">
                  <span className="app-icon">
                    <AppIcon app={c.provider} />
                  </span>
                  <div className="app-name">
                    <h3>{names[c.provider]}</h3>
                    <p>{c.label}</p>
                  </div>
                  <span
                    className={`connection-label ${c.error ? "error-text" : ""}`}
                  >
                    {c.error ? "Needs attention" : "Connected"}
                  </span>
                </div>
                <div className="app-details">
                  <p className="current-status">
                    {c.status === "away" ? (
                      <>🌴 {c.appliedMessage}</>
                    ) : c.status === "unknown" ? (
                      c.error ? (
                        "Status not confirmed."
                      ) : (
                        "No update made yet."
                      )
                    ) : (
                      "Away status cleared."
                    )}
                  </p>
                  {c.provider === "calendar" && c.awayUntil && (
                    <p className="small">
                      Until{" "}
                      {new Date(c.awayUntil).toLocaleString([], {
                        dateStyle: "medium",
                        timeStyle: "short",
                      })}
                    </p>
                  )}
                  {c.error && (
                    <div className="connection-error">
                      <p>{c.error}</p>
                      {c.needsReconnect ? (
                        <a
                          href={`/auth/${c.provider}?connection=${encodeURIComponent(c.id)}`}
                        >
                          Reconnect {c.label} <ExternalLink size={13} />
                        </a>
                      ) : (
                        <button
                          className="text-button"
                          disabled={busy}
                          onClick={() => startUpdate(desired)}
                        >
                          Retry updates
                        </button>
                      )}
                    </div>
                  )}
                </div>
              </article>
            ))}
          </div>
          <button
            className="text-button add-app"
            disabled={loading || busy}
            onClick={() => setModal("connect")}
          >
            <Plus size={16} /> Add an app
          </button>
        </section>

        <section className="explanation" aria-labelledby="how-heading">
          <h2 id="how-heading">How it works</h2>
          <p>
            Connect your accounts and choose an away message for each. Press{" "}
            <strong>I’m away</strong> when you’re unavailable, then{" "}
            <strong>I’m back</strong> when you return.
          </p>
          <p>
            Slack and GitHub show your status. Gmail sends vacation replies.
            Calendar blocks time until your return, without declining
            invitations. Notifications and working hours stay as you set them.
          </p>
        </section>
      </main>
      <footer>
        <a href="/privacy">Privacy</a>
        <a href="/terms">Terms</a>
        <a href="mailto:maxhalford25@gmail.com">Support</a>
      </footer>
      <dialog
        ref={dialog}
        onCancel={() => setModal(null)}
        onClick={(e) => {
          if (e.target === dialog.current) setModal(null);
        }}
        aria-labelledby="dialog-title"
      >
        <div className="modal">
          <button
            className="modal-close"
            onClick={() => setModal(null)}
            aria-label="Close dialog"
          >
            <X size={20} />
          </button>
          {modal === "connect" && (
            <>
              <h2 id="dialog-title">Add an app</h2>
              <p>
                Choose an app to connect. You can add multiple accounts for each
                app.
              </p>
              {connectionButtons}
              {state.googleTesting && (
                <p className="settings-note">
                  Google connections are in testing. Only invited testers can
                  connect while we prepare public verification.
                </p>
              )}
              <div className="privacy-note">
                <ShieldCheck size={16} />
                <span>
                  Gmail access is used for vacation settings; Calendar access is
                  used for absences created here.{" "}
                  <a href="/privacy">How we use your data</a>.
                </span>
              </div>
            </>
          )}
          {modal === "return" && (
            <>
              <h2 id="dialog-title">When will you be back?</h2>
              <p>
                Google Calendar needs an end time for your out-of-office event.
                Your other apps stay away until you press I’m back.
              </p>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void update("away", new Date(returnAt).toISOString());
                }}
              >
                <label className="message-field">
                  Return date and time
                  <input
                    type="datetime-local"
                    value={returnAt}
                    min={localDateTime(
                      new Date(Date.now() + 60_000).toISOString(),
                    )}
                    onChange={(e) => setReturnAt(e.target.value)}
                    required
                  />
                </label>
                <p className="settings-note">
                  Your timezone:{" "}
                  {Intl.DateTimeFormat().resolvedOptions().timeZone}. Existing
                  invitations won’t be declined.
                </p>
                <button className="main-button" disabled={busy}>
                  {busy ? "Updating…" : "Set all apps to away"}
                </button>
              </form>
            </>
          )}
          {modal === "delete" && (
            <>
              <h2 id="dialog-title">Delete your account?</h2>
              <p>
                This permanently deletes your account, saved messages,
                connections and sign-in sessions here.
              </p>
              <p>
                It leaves your current statuses, Gmail vacation replies and
                Calendar events in place. Use I’m back first, or clear them
                directly in each app. You can also revoke access in each
                provider’s settings.
              </p>
              <div className="delete-actions">
                <button
                  className="main-button"
                  disabled={busy}
                  onClick={deleteAccount}
                >
                  Delete account and data
                </button>
                <button
                  className="text-button"
                  disabled={busy}
                  onClick={() => setModal("settings")}
                >
                  Cancel
                </button>
              </div>
            </>
          )}
          {modal === "settings" && (
            <>
              <h2 id="dialog-title">Away messages</h2>
              <p>Choose the message each app shows when you’re away.</p>
              <form onSubmit={saveMessages}>
                {connections.map((c) => (
                  <label className="message-field" key={c.id}>
                    <span>
                      <AppIcon app={c.provider} />
                      {names[c.provider]}
                      <small>
                        {(messages[c.id] || "").length}/{limits[c.provider]}
                      </small>
                    </span>
                    <span className="message-account">{c.label}</span>
                    <div className="input-wrap">
                      {c.provider !== "gmail" && <span>🌴</span>}
                      {c.provider === "gmail" ? (
                        <textarea
                          value={messages[c.id] || ""}
                          onChange={(e) =>
                            setMessages({ ...messages, [c.id]: e.target.value })
                          }
                          maxLength={limits.gmail}
                          required
                          rows={4}
                          aria-label={`${names[c.provider]} ${c.label} away message`}
                        />
                      ) : (
                        <input
                          value={messages[c.id] || ""}
                          onChange={(e) =>
                            setMessages({ ...messages, [c.id]: e.target.value })
                          }
                          maxLength={limits[c.provider]}
                          required
                          aria-label={`${names[c.provider]} ${c.label} away message`}
                        />
                      )}
                    </div>
                  </label>
                ))}
                {!connections.length && (
                  <p>Connect an account to choose its away message.</p>
                )}
                <div className="settings-note">
                  Changes apply the next time you switch away.
                </div>
                <button
                  className="main-button"
                  disabled={busy || !connections.length}
                >
                  {busy ? (
                    <LoaderCircle className="spin" size={17} />
                  ) : (
                    <Check size={17} />
                  )}{" "}
                  Save messages
                </button>
              </form>
              {state.authenticated && (
                <div className="manage-connections">
                  <h3>Manage connections</h3>
                  {connections.map((c) => (
                    <div key={c.id}>
                      <span>
                        {names[c.provider]} · {c.label}
                      </span>
                      {disconnecting === c.id ? (
                        <span className="disconnect-confirm">
                          <button disabled={busy} onClick={() => disconnect(c)}>
                            Clear status & disconnect
                          </button>
                          <button onClick={() => setDisconnecting(null)}>
                            Cancel
                          </button>
                        </span>
                      ) : (
                        <button
                          disabled={busy}
                          onClick={() => setDisconnecting(c.id)}
                        >
                          <Unplug size={13} /> Disconnect
                        </button>
                      )}
                    </div>
                  ))}
                  <p>
                    Disconnecting clears your status first. Removing your last
                    connected account also removes your account here.
                  </p>
                  <button className="logout" onClick={logout} disabled={busy}>
                    <LogOut size={14} /> Sign out
                  </button>
                  <button
                    className="delete-account"
                    onClick={() => setModal("delete")}
                    disabled={busy}
                  >
                    Delete account and data
                  </button>
                </div>
              )}
            </>
          )}
          {notice && (
            <p className="modal-notice" role="status">
              {notice}
            </p>
          )}
        </div>
      </dialog>
    </div>
  );
}
