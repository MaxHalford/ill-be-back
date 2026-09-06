import { useEffect, useRef, useState } from "react";
import {
  Check,
  ExternalLink,
  LoaderCircle,
  LogOut,
  RefreshCw,
  ShieldCheck,
  Unplug,
  X,
} from "lucide-react";
import { getState, request } from "./api";
import { AppIcon } from "./icons";
import type { AppState, Availability, Connection, Provider } from "./types";

const names: Record<Provider, string> = { github: "GitHub", slack: "Slack" };
const empty: AppState = {
  authenticated: false,
  name: "",
  desiredStatus: "available",
  connections: [],
  providers: { github: false, slack: false },
  csrfToken: "",
};
const previewConnections: Connection[] = [
  {
    provider: "slack",
    label: "Your workspace",
    message: "Out of office",
    appliedMessage: "",
    status: "available",
    error: "",
    needsReconnect: false,
    updatedAt: null,
  },
  {
    provider: "github",
    label: "Your profile",
    message: "Out of office",
    appliedMessage: "",
    status: "available",
    error: "",
    needsReconnect: false,
    updatedAt: null,
  },
];

export default function App() {
  const [state, setState] = useState<AppState>(empty);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState(false);
  const [demoConnections, setDemoConnections] = useState(previewConnections);
  const [demoStatus, setDemoStatus] = useState<Availability>("available");
  const [notice, setNotice] = useState("");
  const [modal, setModal] = useState<"settings" | "connect" | null>(null);
  const [messages, setMessages] = useState<Record<Provider, string>>({
    github: "Out of office",
    slack: "Out of office",
  });
  const [disconnecting, setDisconnecting] = useState<Provider | null>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const connections = preview ? demoConnections : state.connections;
  const desired = preview ? demoStatus : state.desiredStatus;
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
      if (!document.hidden && !busy && !preview)
        getState()
          .then(setState)
          .catch(() => {});
    };
    document.addEventListener("visibilitychange", refresh);
    return () => document.removeEventListener("visibilitychange", refresh);
  }, [busy, preview]);

  function openSettings() {
    setMessages({
      github:
        connections.find((c) => c.provider === "github")?.message ||
        "Out of office",
      slack:
        connections.find((c) => c.provider === "slack")?.message ||
        "Out of office",
    });
    setDisconnecting(null);
    setModal("settings");
  }

  async function update(target: Availability) {
    if (!preview && !state.authenticated) {
      setModal("connect");
      return;
    }
    setBusy(true);
    setNotice("");
    try {
      if (preview) {
        setDemoStatus(target);
        setDemoConnections((cs) =>
          cs.map((c) => ({
            ...c,
            status: target,
            appliedMessage: c.message,
            updatedAt: new Date().toISOString(),
          })),
        );
      } else
        setState(
          await request<AppState>("status", state.csrfToken, {
            status: target,
          }),
        );
    } catch (e) {
      setNotice((e as Error).message);
      if (!preview) {
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
      }
    } finally {
      setBusy(false);
    }
  }

  async function saveMessages(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setNotice("");
    try {
      if (preview)
        setDemoConnections((cs) =>
          cs.map((c) => ({ ...c, message: messages[c.provider].trim() })),
        );
      else
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

  async function disconnect(provider: Provider) {
    setBusy(true);
    setNotice("");
    try {
      setState(
        await request<AppState>(
          `connections/${provider}`,
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
          c.provider === provider
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

  const connectionButtons = (
    <div className="connect-options">
      {(["slack", "github"] as Provider[]).map((provider) => (
        <a
          key={provider}
          className={`connect-option ${!state.providers[provider] ? "disabled" : ""}`}
          href={state.providers[provider] ? `/auth/${provider}` : undefined}
          aria-disabled={!state.providers[provider]}
        >
          <AppIcon app={provider} />
          <span>
            Continue with {names[provider]}
            {!state.providers[provider] && <small>Not available yet</small>}
          </span>
        </a>
      ))}
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
            state.authenticated || preview
              ? openSettings()
              : setModal("connect")
          }
        >
          {state.authenticated
            ? "Account & settings"
            : preview
              ? "Settings"
              : "Sign in"}
        </button>
      </header>
      <main>
        <p className="introduction">
          Let people know when you’re away. Set your out-of-office status in
          Slack and GitHub with one button.
        </p>

        {preview && (
          <div className="preview-banner">
            <p>
              <strong>Preview.</strong> No real accounts or statuses will
              change.
            </p>
            <button
              className="text-button"
              onClick={() => {
                setPreview(false);
                setNotice("");
              }}
            >
              Exit preview
            </button>
          </div>
        )}
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
                  : "Ready to update your apps."
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
                  ? "Your away statuses will stay on until you come back."
                  : "Taking some time off? Let your apps know."}
          </p>
          <button
            className="primary-button"
            disabled={loading || busy}
            onClick={() => update(away ? "available" : "away")}
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
                {succeeded} of {connections.length} apps{" "}
                {away ? "set to away" : "cleared"}
                {preview ? " (preview)" : ""}.
              </span>
              {!synced && (
                <button
                  className="text-button"
                  disabled={busy}
                  onClick={() => update(desired)}
                >
                  {failed ? "Retry updates" : "Update apps"}{" "}
                  <RefreshCw size={13} />
                </button>
              )}
            </div>
          )}
          <p className="small">
            “I’m back” clears your away statuses. No timers or schedules.
          </p>
        </section>

        <section className="apps-section" aria-labelledby="apps-heading">
          <div className="section-heading">
            <h2 id="apps-heading">Your apps</h2>
            <button className="text-button" onClick={openSettings}>
              Edit away messages
            </button>
          </div>
          <div className="app-list">
            {(["slack", "github"] as Provider[]).map((provider) => {
              const c = connections.find((item) => item.provider === provider);
              return (
                <article className="app-row" key={provider}>
                  <div className="app-row-heading">
                    <span className="app-icon">
                      <AppIcon app={provider} />
                    </span>
                    <div className="app-name">
                      <h3>{names[provider]}</h3>
                      <p>{c ? c.label : "Not connected"}</p>
                    </div>
                    {c ? (
                      <span
                        className={`connection-label ${c.error ? "error-text" : ""}`}
                      >
                        {c.error
                          ? "Needs attention"
                          : preview
                            ? "Preview"
                            : "Connected"}
                      </span>
                    ) : (
                      <button
                        className="text-button"
                        onClick={() => setModal("connect")}
                      >
                        Connect
                        <span className="sr-only"> {names[provider]}</span>
                      </button>
                    )}
                  </div>
                  {c && (
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
                      {c.error && (
                        <div className="connection-error">
                          <p>{c.error}</p>
                          {c.needsReconnect ? (
                            <a href={`/auth/${provider}`}>
                              Reconnect {names[provider]}{" "}
                              <ExternalLink size={13} />
                            </a>
                          ) : (
                            <button
                              className="text-button"
                              disabled={busy}
                              onClick={() => update(desired)}
                            >
                              Try again
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                </article>
              );
            })}
          </div>
          <p className="small coming-next">
            Gmail and Google Calendar are next.
          </p>
        </section>

        <section className="explanation" aria-labelledby="how-heading">
          <h2 id="how-heading">How it works</h2>
          <p>
            Connect your apps and choose an away message for each. Press{" "}
            <strong>I’m away</strong> when you’re unavailable, then{" "}
            <strong>I’m back</strong> when you return.
          </p>
          <p>
            This updates your profile statuses. It doesn’t mute notifications or
            change your working hours.
          </p>
          {!state.authenticated && !preview && (
            <button
              className="text-button"
              onClick={() => {
                setPreview(true);
                setNotice("");
              }}
            >
              Try it without connecting an account
            </button>
          )}
        </section>
      </main>
      <footer>A small tool for being clear about your availability.</footer>
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
              <h2 id="dialog-title">Connect an app</h2>
              <p>
                Sign in with an app to connect it. Add the other whenever you’re
                ready.
              </p>
              {connectionButtons}
              <div className="privacy-note">
                <ShieldCheck size={16} />
                <span>
                  We only update your status. Your messages and repositories
                  stay yours.
                </span>
              </div>
              <button
                className="preview-link"
                onClick={() => {
                  setPreview(true);
                  setModal(null);
                }}
              >
                Try it without connecting
              </button>
            </>
          )}
          {modal === "settings" && (
            <>
              <h2 id="dialog-title">Away messages</h2>
              <p>Choose the message each app shows when you’re away.</p>
              <form onSubmit={saveMessages}>
                {(["slack", "github"] as Provider[]).map((provider) => (
                  <label className="message-field" key={provider}>
                    <span>
                      <AppIcon app={provider} />
                      {names[provider]}{" "}
                      <small>
                        {messages[provider].length}/
                        {provider === "github" ? 80 : 100}
                      </small>
                    </span>
                    <div className="input-wrap">
                      <span>🌴</span>
                      <input
                        disabled={
                          !connections.some((c) => c.provider === provider)
                        }
                        value={messages[provider]}
                        onChange={(e) =>
                          setMessages({
                            ...messages,
                            [provider]: e.target.value,
                          })
                        }
                        maxLength={provider === "github" ? 80 : 100}
                        required
                        aria-label={`${names[provider]} away message`}
                      />
                    </div>
                    {!connections.some((c) => c.provider === provider) && (
                      <a
                        className="field-hint"
                        href={
                          state.providers[provider]
                            ? `/auth/${provider}`
                            : undefined
                        }
                        onClick={(e) => {
                          if (!state.providers[provider]) {
                            e.preventDefault();
                            setModal("connect");
                          }
                        }}
                      >
                        Connect {names[provider]} to edit its message
                      </a>
                    )}
                  </label>
                ))}
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
              {state.authenticated && !preview && (
                <div className="manage-connections">
                  <h3>Manage connections</h3>
                  {connections.map((c) => (
                    <div key={c.provider}>
                      <span>{names[c.provider]}</span>
                      {disconnecting === c.provider ? (
                        <span className="disconnect-confirm">
                          <button
                            disabled={busy}
                            onClick={() => disconnect(c.provider)}
                          >
                            Clear status & disconnect
                          </button>
                          <button onClick={() => setDisconnecting(null)}>
                            Cancel
                          </button>
                        </span>
                      ) : (
                        <button
                          disabled={busy}
                          onClick={() => setDisconnecting(c.provider)}
                        >
                          <Unplug size={13} /> Disconnect
                        </button>
                      )}
                    </div>
                  ))}
                  <p>
                    Disconnecting clears your status first. Removing your last
                    app also removes your account here.
                  </p>
                  <button className="logout" onClick={logout} disabled={busy}>
                    <LogOut size={14} /> Sign out
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
