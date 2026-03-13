import { writable } from "svelte/store";

type AuthSession = {
  authRequired: boolean;
  authenticated: boolean;
};

export const authReady = writable(false);
export const authRequired = writable(false);
export const isAuthenticated = writable(false);
export const authSubmitting = writable(false);
export const authError = writable<string | null>(null);

async function parseError(response: Response, fallback: string): Promise<string> {
  try {
    const data = await response.clone().json() as { error?: string };
    if (typeof data.error === "string" && data.error.length > 0) {
      return data.error;
    }
  } catch {}

  try {
    const text = await response.text();
    if (text.length > 0) {
      return text;
    }
  } catch {}

  return fallback;
}

function applySession(session: AuthSession): void {
  authRequired.set(session.authRequired);
  isAuthenticated.set(session.authenticated);
}

export async function initializeAuth(): Promise<void> {
  authReady.set(false);
  authError.set(null);

  try {
    const response = await fetch("/api/auth/session", {
      cache: "no-store",
      headers: {
        Accept: "application/json",
      },
    });

    if (!response.ok) {
      throw new Error(await parseError(response, "Failed to load authentication state"));
    }

    applySession(await response.json() as AuthSession);
  } catch (error) {
    authRequired.set(true);
    isAuthenticated.set(false);
    authError.set(error instanceof Error ? error.message : "Failed to load authentication state");
  } finally {
    authReady.set(true);
  }
}

export async function login(password: string): Promise<boolean> {
  authSubmitting.set(true);
  authError.set(null);

  try {
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ password }),
    });

    if (!response.ok) {
      const message = await parseError(response, "Login failed");
      authRequired.set(true);
      isAuthenticated.set(false);
      authError.set(message);
      return false;
    }

    applySession(await response.json() as AuthSession);
    return true;
  } catch (error) {
    authRequired.set(true);
    isAuthenticated.set(false);
    authError.set(error instanceof Error ? error.message : "Login failed");
    return false;
  } finally {
    authReady.set(true);
    authSubmitting.set(false);
  }
}

export async function logout(): Promise<void> {
  try {
    await fetch("/api/auth/logout", {
      method: "POST",
      headers: {
        Accept: "application/json",
      },
    });
  } catch {}

  authRequired.set(true);
  isAuthenticated.set(false);
  authReady.set(true);
  authError.set(null);
}

export function handleUnauthorized(): void {
  authRequired.set(true);
  isAuthenticated.set(false);
  authReady.set(true);
  authError.set("Session expired. Sign in again.");
}
