import { useEffect, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowRight,
  Check,
  CheckCheck,
  ChevronRight,
  CircleHelp,
  DoorOpen,
  ExternalLink,
  LoaderCircle,
  LogOut,
  Monitor,
  Palmtree,
  Plug,
  RefreshCw,
  Settings2,
  ShieldCheck,
  Sparkles,
  Unplug,
  X,
} from "lucide-react";
import { getState, request } from "./api";
import { AppIcon, PixelMark } from "./icons";
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
  const [modal, setModal] = useState<"settings" | "connect" | "help" | null>(
    null,
  );
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
      if (!preview)
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
      {(["slack", "github"] as Provider[]).map((p) => (
        <a
          key={p}
          className={`connect-option ${!state.providers[p] ? "disabled" : ""}`}
          href={state.providers[p] ? `/auth/${p}` : undefined}
          aria-disabled={!state.providers[p]}
        >
          <span className="app-icon">
            <AppIcon app={p} />
          </span>
          <span>
            Continue with {names[p]}
            {!state.providers[p] && (
              <small>Available once the app is set up</small>
            )}
          </span>
          <ArrowRight size={18} />
        </a>
      ))}
    </div>
  );

  return (
    <div className={`app ${away ? "is-away" : ""}`}>
      <header className="header">
        <a href="/" className="brand">
          <span className="brand-mark">
            <PixelMark />
          </span>
          <span>
            i’ll be back<span className="brand-period">.</span>
          </span>
        </a>
        <nav aria-label="Main navigation">
          <a href="#dashboard" className="nav-active">
            My status
          </a>
          <a href="#apps">My apps</a>
        </nav>
        <button
          className="header-action"
          onClick={() =>
            state.authenticated || preview
              ? openSettings()
              : setModal("connect")
          }
        >
          <span className="avatar">
            {state.name ? state.name[0].toUpperCase() : <PixelMark />}
          </span>
          <span>
            {preview
              ? "Preview mode"
              : state.authenticated
                ? state.name
                : "Get started"}
          </span>
          <ChevronRight size={15} />
        </button>
      </header>

      <main id="dashboard">
        {preview && (
          <div className="preview-banner">
            <span>
              <Sparkles size={15} /> You’re taking a test drive. No real
              statuses will change.
            </span>
            <button
              onClick={() => {
                setPreview(false);
                setNotice("");
              }}
            >
              Exit preview <X size={14} />
            </button>
          </div>
        )}
        <section className="intro">
          <div>
            <div className="eyebrow">
              <span /> LESS STATUS UPDATING. MORE LIVING.
            </div>
            <h1>
              Your time. <span>On your terms.</span>
            </h1>
            <p>
              One switch to let everyone know you’re away. Go do your thing.
            </p>
          </div>
          <button className="how-button" onClick={() => setModal("help")}>
            <CircleHelp size={16} /> How it works
          </button>
        </section>

        {notice && (
          <div className="notice" role="status">
            <span>{notice}</span>
            <button aria-label="Dismiss message" onClick={() => setNotice("")}>
              <X size={16} />
            </button>
          </div>
        )}

        <section className="dashboard-grid" aria-label="Availability controls">
          <div className="scene-card">
            <div className="scene-top">
              <span className="scene-label">
                <span className={`led ${away ? "amber" : ""}`} />
                {away ? "VACATION PROTOCOL" : "WORK MODE: ACTIVATED"}
              </span>
              <span className="scene-number">
                {away ? "02 / 02" : "01 / 02"}
              </span>
            </div>
            <div
              role="img"
              aria-label={
                away
                  ? "Pixel-art Schwarzenegger relaxing on a tropical beach in a Hawaiian shirt"
                  : "Pixel-art Schwarzenegger in sunglasses and a leather jacket against a futuristic city"
              }
              className={`scene-art ${away ? "scene-away" : ""}`}
            />
            <div className="scene-caption">
              <span>
                {away
                  ? "Hasta la vista, meetings."
                  : "All systems operational."}
              </span>
              <span className="pixel-spark">✦</span>
            </div>
          </div>

          <div className="control-card">
            <div className="control-top">
              <span className="eyebrow">YOUR AVAILABILITY</span>
              <span className={`status-pill ${away ? "away" : ""}`}>
                <span className="led" />
                {connections.length && !synced
                  ? failed
                    ? "Needs attention"
                    : "Ready to sync"
                  : away
                    ? "Away"
                    : "Available"}
              </span>
            </div>
            <div className="control-copy">
              <div className="mode-icon">
                {away ? (
                  <Palmtree size={27} strokeWidth={1.5} />
                ) : (
                  <Monitor size={26} strokeWidth={1.5} />
                )}
              </div>
              <h2>
                {away ? (
                  <>
                    You’ll be back.
                    <br />
                    Go enjoy yourself.
                  </>
                ) : (
                  <>
                    On the clock.
                    <br />
                    Until you’re not.
                  </>
                )}
              </h2>
              <p>
                {away
                  ? synced
                    ? "Your apps know you’re out. Your time is yours until you switch back."
                    : "Your away switch is on. Check your apps below to see which statuses updated."
                  : "Ready for a break? Let your work apps do the explaining."}
              </p>
            </div>
            <div className="control-bottom">
              <button
                className="main-button"
                disabled={loading || busy}
                onClick={() => update(away ? "available" : "away")}
              >
                {busy ? (
                  <LoaderCircle className="spin" size={20} />
                ) : away ? (
                  <ArrowRight size={20} />
                ) : (
                  <DoorOpen size={20} />
                )}
                <span>
                  {loading
                    ? "Getting ready…"
                    : busy
                      ? "Updating your apps…"
                      : away
                        ? "I’m back"
                        : "I’m away"}
                </span>
                <span className="button-key">{away ? "↵" : "→"}</span>
              </button>
              <p className="button-note">
                {away
                  ? "One click to clear your away statuses."
                  : "No timer. Come back when you’re ready."}
              </p>
              <div className="sync-line" aria-live="polite">
                {connections.length ? (
                  <>
                    <CheckCheck size={16} />
                    <span>
                      {succeeded} of {connections.length} apps{" "}
                      {away ? "set to away" : "ready"}
                      {preview && " · preview"}
                    </span>
                    {!synced && (
                      <button disabled={busy} onClick={() => update(desired)}>
                        {failed ? "Retry" : "Sync apps"} <RefreshCw size={12} />
                      </button>
                    )}
                  </>
                ) : (
                  <>
                    <ShieldCheck size={15} />
                    <span>Connect your apps to get started</span>
                  </>
                )}
              </div>
            </div>
          </div>
        </section>

        <section className="apps-section" id="apps">
          <div className="section-heading">
            <div>
              <h3>
                Your apps{" "}
                <span>{connections.length.toString().padStart(2, "0")}</span>
              </h3>
              <p>Your availability, in all the right places.</p>
            </div>
            <button className="text-button" onClick={openSettings}>
              <Settings2 size={15} /> Customize messages
            </button>
          </div>
          <div className="apps-grid">
            {(["slack", "github"] as Provider[]).map((provider) => {
              const c = connections.find((item) => item.provider === provider);
              return (
                <article
                  className={`app-card ${c?.error ? "has-error" : ""}`}
                  key={provider}
                >
                  <div className="app-card-header">
                    <span className="app-icon">
                      <AppIcon app={provider} />
                    </span>
                    <div>
                      <h4>{names[provider]}</h4>
                      <span className="account-label">
                        {c
                          ? c.label
                          : provider === "slack"
                            ? "Keep your workspace in the know"
                            : "Let collaborators know you’re away"}
                      </span>
                    </div>
                    {c && (
                      <span
                        className={`connection-badge ${c.error ? "error" : ""}`}
                      >
                        {c.error
                          ? "Needs attention"
                          : preview
                            ? "Preview"
                            : "Connected"}
                        {!c.error && <Check size={11} />}
                      </span>
                    )}
                  </div>
                  {c ? (
                    <>
                      <div className="status-preview">
                        <span>
                          {c.status === "away"
                            ? "🌴"
                            : c.status === "unknown"
                              ? "—"
                              : "☕"}
                        </span>
                        <span>
                          {c.status === "away"
                            ? c.appliedMessage
                            : c.status === "unknown"
                              ? "Ready for your first switch"
                              : "No away status"}
                        </span>
                        <span className="preview-label">STATUS</span>
                      </div>
                      {c.error ? (
                        <div className="connection-error">
                          <p>{c.error}</p>
                          {c.needsReconnect ? (
                            <a href={`/auth/${provider}`}>
                              Reconnect {names[provider]}{" "}
                              <ExternalLink size={12} />
                            </a>
                          ) : (
                            <button
                              disabled={busy}
                              onClick={() => update(desired)}
                            >
                              Try again <RefreshCw size={12} />
                            </button>
                          )}
                        </div>
                      ) : (
                        <div className="app-card-footer">
                          <span>
                            <span
                              className={`mini-dot ${c.status === "away" ? "amber" : ""}`}
                            />
                            {c.updatedAt
                              ? "Last update confirmed"
                              : "Ready when you are"}
                          </span>
                          <button
                            onClick={openSettings}
                            aria-label={`Edit ${names[provider]} message`}
                          >
                            <Settings2 size={15} />
                          </button>
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="unconnected">
                      <span>Make your next break a little easier.</span>
                      <button onClick={() => setModal("connect")}>
                        Connect <Plug size={14} />
                      </button>
                    </div>
                  )}
                </article>
              );
            })}
          </div>
          <div className="coming-next">
            <div className="coming-icons">
              <AppIcon app="gmail" />
              <AppIcon app="calendar" />
            </div>
            <span>
              <strong>More apps. Same little switch.</strong> Gmail & Google
              Calendar are up next.
            </span>
            <span className="soon-badge">COMING SOON</span>
          </div>
        </section>

        {!state.authenticated && !preview && (
          <div className="try-preview">
            <span>Curious what clocking out looks like?</span>
            <button
              onClick={() => {
                setPreview(true);
                setNotice("");
                window.scrollTo({ top: 0, behavior: "smooth" });
              }}
            >
              Take a test drive <ArrowRight size={15} />
            </button>
          </div>
        )}
        <footer>
          <span>
            <PixelMark /> A little less online. A little more life.
          </span>
          <span>Made for humans. And the occasional cyborg.</span>
        </footer>
      </main>

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
              <span className="modal-symbol">
                <Plug size={25} />
              </span>
              <div className="eyebrow">A ONE-TIME HELLO</div>
              <h2 id="dialog-title">Bring your apps along.</h2>
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
                Just looking? Try the preview <ArrowRight size={14} />
              </button>
            </>
          )}
          {modal === "settings" && (
            <>
              <div className="eyebrow">MAKE IT YOURS</div>
              <h2 id="dialog-title">A word before you go.</h2>
              <p>
                Choose what people see when you’re away. Keep it professional.
                Or keep it you.
              </p>
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
                        Connect {names[provider]} to customize{" "}
                        <ArrowRight size={12} />
                      </a>
                    )}
                  </label>
                ))}
                <div className="settings-note">
                  <CircleHelp size={15} /> Changes apply the next time you
                  switch away.
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
          {modal === "help" && (
            <>
              <div className="eyebrow">THE SHORT VERSION</div>
              <h2 id="dialog-title">
                Less admin.
                <br />
                More out of office.
              </h2>
              <div className="steps">
                <div>
                  <span>01</span>
                  <section>
                    <h3>Connect your work apps</h3>
                    <p>
                      Link Slack, GitHub, or both. Set your away messages once.
                    </p>
                  </section>
                </div>
                <ArrowDown size={17} />
                <div>
                  <span>02</span>
                  <section>
                    <h3>Make your exit</h3>
                    <p>
                      Hit “I’m away.” We set your status in every connected app
                      and show you the results.
                    </p>
                  </section>
                </div>
                <ArrowDown size={17} />
                <div>
                  <span>03</span>
                  <section>
                    <h3>Come back on your terms</h3>
                    <p>
                      Hit “I’m back” to clear your away statuses. There’s no
                      schedule, timer, or notification setting to manage.
                    </p>
                  </section>
                </div>
              </div>
              <button className="main-button" onClick={() => setModal(null)}>
                Sounds good <Check size={17} />
              </button>
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
