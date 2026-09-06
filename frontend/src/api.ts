import type { AppState } from "./types";

export async function request<T>(
  path: string,
  csrf = "",
  body?: unknown,
  method = "POST",
): Promise<T> {
  const response = await fetch(`/api/${path}`, {
    method: body === undefined && method === "POST" ? "GET" : method,
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    credentials: "same-origin",
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const data = await response.json();
  if (!response.ok)
    throw new Error(data.error || "Something went wrong. Please try again.");
  return data as T;
}

export const getState = () => request<AppState>("state");
