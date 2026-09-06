export type Provider = "github" | "slack";
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
};
export type AppState = {
  authenticated: boolean;
  name: string;
  desiredStatus: Availability;
  connections: Connection[];
  providers: Record<Provider, boolean>;
  csrfToken: string;
};
