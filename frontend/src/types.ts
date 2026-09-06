export type Provider = "github" | "slack" | "gmail" | "calendar";
export type Availability = "available" | "away";
export type Connection = {
  id: string;
  provider: Provider;
  label: string;
  message: string;
  appliedMessage: string;
  status: Availability | "unknown";
  error: string;
  needsReconnect: boolean;
  updatedAt: string | null;
  awayUntil?: string;
};
export type AppState = {
  authenticated: boolean;
  name: string;
  desiredStatus: Availability;
  returnAt: string;
  connections: Connection[];
  providers: Record<Provider, boolean>;
  csrfToken: string;
  googleTesting?: boolean;
};
