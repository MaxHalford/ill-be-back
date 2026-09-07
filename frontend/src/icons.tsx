import type { Provider } from "./types";

export function AppIcon({ app }: { app: Provider }) {
  if (app === "teams")
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <circle cx="15" cy="5" r="3" fill="#7b83eb" />
        <circle cx="21" cy="7" r="2" fill="#5059c9" />
        <path d="M18 11h6v5a3 3 0 0 1-6 0z" fill="#5059c9" />
        <path d="M8 10h12v7a6 6 0 0 1-12 0z" fill="#7b83eb" />
        <rect x="1" y="7" width="12" height="13" rx="1" fill="#464eb8" />
        <path d="M4 10h6v2H8v6H6v-6H4z" fill="white" />
      </svg>
    );
  if (app === "outlook")
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <rect x="8" y="2" width="13" height="17" rx="1" fill="#0078d4" />
        <path d="M10 5h8v3h-8z" fill="#50d9ff" />
        <path d="M8 11h15v10H8z" fill="#28a8ea" />
        <path d="m8 11 7.5 6 7.5-6v10H8z" fill="#1490df" />
        <rect x="1" y="6" width="12" height="14" rx="1" fill="#106ebe" />
        <ellipse
          cx="7"
          cy="13"
          rx="3"
          ry="4"
          fill="none"
          stroke="white"
          strokeWidth="1.8"
        />
      </svg>
    );
  if (app === "github")
    return (
      <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <path d="M12 .7a11.5 11.5 0 0 0-3.64 22.41c.58.11.79-.25.79-.56v-2.23c-3.21.7-3.89-1.36-3.89-1.36-.53-1.34-1.29-1.7-1.29-1.7-1.05-.72.08-.71.08-.71 1.16.08 1.77 1.18 1.77 1.18 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.56-.29-5.25-1.28-5.25-5.69 0-1.26.45-2.29 1.18-3.09-.12-.3-.51-1.46.11-3.04 0 0 .97-.31 3.16 1.18a10.95 10.95 0 0 1 5.76 0c2.2-1.49 3.16-1.18 3.16-1.18.63 1.58.24 2.74.12 3.04.74.8 1.18 1.83 1.18 3.09 0 4.43-2.69 5.39-5.26 5.68.42.36.78 1.06.78 2.14v3.28c0 .31.21.68.79.56A11.5 11.5 0 0 0 12 .7Z" />
      </svg>
    );
  if (app === "slack")
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <g fill="#36C5F0">
          <rect x="1" y="9" width="10" height="4" rx="2" />
          <rect x="7" y="1" width="4" height="7" rx="2" />
        </g>
        <g fill="#2EB67D">
          <rect x="12" y="1" width="4" height="10" rx="2" />
          <rect x="17" y="7" width="6" height="4" rx="2" />
        </g>
        <g fill="#ECB22E">
          <rect x="13" y="12" width="10" height="4" rx="2" />
          <rect x="13" y="17" width="4" height="6" rx="2" />
        </g>
        <g fill="#E01E5A">
          <rect x="8" y="13" width="4" height="10" rx="2" />
          <rect x="1" y="14" width="6" height="4" rx="2" />
        </g>
      </svg>
    );
  if (app === "gmail")
    return (
      <svg viewBox="0 0 24 24" fill="none" strokeWidth="3.5" aria-hidden="true">
        <path d="M3 20V5l9 7 9-7v15" stroke="#4285f4" />
        <path d="M3 20V5l9 7" stroke="#ea4335" />
        <path d="m12 12 9-7v15" stroke="#34a853" />
        <path d="m3 5 4 3" stroke="#c5221f" />
        <path d="m17 8 4-3" stroke="#fbbc04" />
      </svg>
    );
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <rect x="2" y="2" width="20" height="20" rx="3" fill="#4285f4" />
      <path d="M6 6h12v12H6z" fill="white" />
      <path d="M2 18h16v4H5a3 3 0 0 1-3-3" fill="#34a853" />
      <path d="M18 6h4v12h-4z" fill="#fbbc04" />
      <text
        x="7"
        y="15.5"
        fill="#4285f4"
        fontSize="9"
        fontFamily="sans-serif"
        fontWeight="bold"
      >
        31
      </text>
    </svg>
  );
}
