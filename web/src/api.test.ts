import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  api,
  AUTHORIZATION_CHANGED_EVENT,
  clearTokens,
  getAccessToken,
  getRefreshToken,
  saveTokens,
  SESSION_INVALIDATED_EVENT
} from "./api";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" }
  });
}

describe("API session lifecycle", () => {
  beforeEach(() => {
    clearTokens();
    vi.restoreAllMocks();
  });

  it("uses one refresh for concurrent 401 responses and retries both requests", async () => {
    saveTokens({ access_token: "old-access", refresh_token: "old-refresh" });
    let refreshCalls = 0;
    let protectedCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const target = String(input);
        if (target.endsWith("/api/v1/auth/refresh")) {
          refreshCalls += 1;
          await Promise.resolve();
          return jsonResponse(200, { success: true, data: { access_token: "new-access", refresh_token: "new-refresh" } });
        }
        if (target.endsWith("/api/v1/auth/me")) {
          protectedCalls += 1;
          const authorization = new Headers(init?.headers).get("Authorization");
          if (authorization === "Bearer old-access") {
            return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "expired" } });
          }
          return jsonResponse(200, {
            success: true,
            data: { id: 1, email: "user@example.com", first_name: "U", last_name: "S", role: "RENT", is_blocked: false }
          });
        }
        throw new Error(`unexpected request ${target}`);
      })
    );

    const [first, second] = await Promise.all([api.me(), api.me()]);
    expect(first.id).toBe(1);
    expect(second.id).toBe(1);
    expect(refreshCalls).toBe(1);
    expect(protectedCalls).toBe(4);
    expect(getAccessToken()).toBe("new-access");
    expect(getRefreshToken()).toBe("new-refresh");
  });

  it("clears and broadcasts once when refresh fails", async () => {
    saveTokens({ access_token: "expired-access", refresh_token: "expired-refresh" });
    let invalidated = 0;
    window.addEventListener(SESSION_INVALIDATED_EVENT, () => (invalidated += 1), { once: true });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        if (String(input).endsWith("/api/v1/auth/refresh")) {
          return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "revoked" } });
        }
        return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "expired" } });
      })
    );

    await expect(api.me()).rejects.toMatchObject({ status: 401 });
    expect(getAccessToken()).toBeNull();
    expect(getRefreshToken()).toBeNull();
    expect(invalidated).toBe(1);
  });

  it("does not resurrect a cleared session when an in-flight refresh finishes", async () => {
    saveTokens({ access_token: "expired-access", refresh_token: "refresh-before-logout" });
    let releaseRefresh!: () => void;
    let markRefreshStarted!: () => void;
    const refreshGate = new Promise<void>((resolve) => {
      releaseRefresh = resolve;
    });
    const refreshStarted = new Promise<void>((resolve) => {
      markRefreshStarted = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        if (String(input).endsWith("/api/v1/auth/refresh")) {
          markRefreshStarted();
          await refreshGate;
          return jsonResponse(200, { success: true, data: { access_token: "late-access", refresh_token: "late-refresh" } });
        }
        return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "expired" } });
      })
    );

    const pending = api.me();
    await refreshStarted;
    clearTokens();
    releaseRefresh();
    await expect(pending).rejects.toMatchObject({ status: 401, code: "SESSION_CHANGED" });
    expect(getAccessToken()).toBeNull();
    expect(getRefreshToken()).toBeNull();
  });

  it("does not refresh an admin 403 and broadcasts an authorization change", async () => {
    saveTokens({ access_token: "stale-admin", refresh_token: "revoked-by-demotion" });
    let changed = 0;
    window.addEventListener(AUTHORIZATION_CHANGED_EVENT, () => (changed += 1), { once: true });
    const fetchMock = vi.fn(async () => jsonResponse(403, { success: false, error: { code: "FORBIDDEN", message: "forbidden" } }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.adminUsers()).rejects.toMatchObject({ status: 403 });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(changed).toBe(1);
  });
});
