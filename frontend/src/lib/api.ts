export type PurchaseStatus = "PENDING" | "APPROVED" | "REJECTED";
export type Decision = Exclude<PurchaseStatus, "PENDING">;
export interface User {
  id: string;
  username: string;
  name: string;
  role: "employee" | "manager" | "admin";
}
export interface AuthSession {
  user: User;
  csrf_token: string;
  expires_at: string;
}

export interface Purchase {
  id: string;
  client_request_id: string;
  employee_id: string;
  item_name: string;
  quantity: number;
  unit_price_cents: number;
  total_cents: number;
  status: PurchaseStatus;
  // The current gateway does not expose dates. Never infer them from UUIDs.
  created_at?: string;
}

export interface CreatePurchase {
  client_request_id: string;
  employee_id: string;
  item_name: string;
  quantity: number;
  unit_price_cents: number;
}

export interface DecisionInput {
  manager_id: string;
  decision: Decision;
  comment: string;
}

export interface PolicyAnswer {
  configured: boolean;
  answer: string;
  sources: {
    document: string;
    section: string;
    chunk_index: number;
    quote: string;
  }[];
}

interface DecisionResult {
  purchase_id: string;
  status: Decision;
  demo_only: false;
  metadata_persisted: false;
}

export class ApiError extends Error {
  constructor(
    message: string,
    public readonly code: string,
    public readonly retryable: boolean,
    public readonly status = 0,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

function object(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function readPurchase(value: unknown): Purchase {
  if (
    !object(value) ||
    !["id", "client_request_id", "employee_id", "item_name"].every(
      (key) => typeof value[key] === "string",
    ) ||
    !["PENDING", "APPROVED", "REJECTED"].includes(String(value.status))
  ) {
    throw new ApiError(
      "The API returned an unexpected purchase response.",
      "INVALID_RESPONSE",
      false,
    );
  }
  for (const key of ["quantity", "unit_price_cents", "total_cents"]) {
    if (!Number.isSafeInteger(value[key]) || Number(value[key]) < 0)
      throw new ApiError(
        "A purchase contains an amount or quantity outside the browser’s exact integer range. It cannot be displayed safely.",
        "UNSAFE_NUMBER",
        false,
      );
  }
  if (
    Number(value.quantity) < 1 ||
    BigInt(value.quantity as number) *
      BigInt(value.unit_price_cents as number) !==
      BigInt(value.total_cents as number)
  )
    throw new ApiError(
      "The API returned an inconsistent purchase total.",
      "INVALID_RESPONSE",
      false,
    );
  const purchase = value as unknown as Purchase;
  return {
    ...purchase,
    created_at:
      typeof value.created_at === "string" &&
      Number.isFinite(Date.parse(value.created_at))
        ? value.created_at
        : undefined,
  };
}

function readUser(value: unknown): User {
  if (
    !object(value) ||
    !["id", "username", "name"].every(
      (key) => typeof value[key] === "string",
    ) ||
    !["employee", "manager", "admin"].includes(String(value.role))
  )
    throw new ApiError(
      "The API returned an invalid user.",
      "INVALID_RESPONSE",
      false,
    );
  return value as unknown as User;
}

export function createApiClient(baseUrl: string, timeoutMs = 12000) {
  let csrf = "";
  function session(value: unknown): AuthSession {
    if (
      !object(value) ||
      !object(value.user) ||
      !["id", "username", "name"].every(
        (key) =>
          typeof (value.user as Record<string, unknown>)[key] === "string",
      ) ||
      !["employee", "manager", "admin"].includes(String(value.user.role)) ||
      typeof value.csrf_token !== "string" ||
      !value.csrf_token ||
      typeof value.expires_at !== "string" ||
      !Number.isFinite(Date.parse(value.expires_at))
    )
      throw new ApiError(
        "The API returned an invalid session.",
        "INVALID_RESPONSE",
        false,
      );
    csrf = value.csrf_token;
    return value as unknown as AuthSession;
  }
  async function request(path: string, body?: unknown): Promise<unknown> {
    let base: URL;
    try {
      base = new URL(baseUrl);
      if (
        !["http:", "https:"].includes(base.protocol) ||
        base.username ||
        base.password ||
        base.search ||
        base.hash
      )
        throw new Error();
    } catch {
      throw new ApiError(
        "VITE_API_BASE_URL must be an HTTP gateway URL without credentials, query, or fragment.",
        "CONFIGURATION",
        false,
      );
    }
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const response = await fetch(
        `${base.toString().replace(/\/$/, "")}${path}`,
        {
          method: body === undefined ? "GET" : "POST",
          headers:
            body === undefined
              ? { Accept: "application/json" }
              : {
                  Accept: "application/json",
                  "Content-Type": "application/json",
                  ...(csrf ? { "X-CSRF-Token": csrf } : {}),
                },
          credentials: "include",
          signal: controller.signal,
          body: body === undefined ? undefined : JSON.stringify(body),
        },
      );
      const data: unknown = await response.json().catch(() => null);
      if (!response.ok) {
        if (response.status === 401 && path !== "/api/auth/login") {
          csrf = "";
          if (typeof window !== "undefined")
            window.dispatchEvent(new Event("session-expired"));
        }
        if (
          object(data) &&
          object(data.error) &&
          typeof data.error.message === "string"
        ) {
          throw new ApiError(
            data.error.message,
            String(data.error.code ?? "REQUEST_FAILED"),
            data.error.retryable === true,
            response.status,
          );
        }
        throw new ApiError(
          `The API returned HTTP ${response.status}. Please refresh and try again.`,
          "REQUEST_FAILED",
          response.status >= 500,
          response.status,
        );
      }
      if (data === null)
        throw new ApiError(
          "The gateway returned an unreadable response.",
          "INVALID_RESPONSE",
          false,
        );
      return data;
    } catch (error) {
      if (error instanceof ApiError) throw error;
      if (controller.signal.aborted)
        throw new ApiError(
          "The request timed out. A submitted change may already exist; retry the same request.",
          "TIMEOUT",
          true,
        );
      throw new ApiError(
        "Could not reach the API. Check the gateway and your connection, then try again.",
        "NETWORK",
        true,
      );
    } finally {
      clearTimeout(timer);
    }
  }

  return {
    async signup(
      username: string,
      name: string,
      password: string,
    ): Promise<User> {
      const data = await request("/api/auth/signup", {
        username,
        name,
        password,
      });
      if (!object(data))
        throw new ApiError(
          "Invalid signup response.",
          "INVALID_RESPONSE",
          false,
        );
      return readUser(data.user);
    },
    async adminUsers(): Promise<User[]> {
      const data = await request("/api/admin/users");
      if (!object(data) || !Array.isArray(data.users))
        throw new ApiError("Invalid user list.", "INVALID_RESPONSE", false);
      return data.users.map(readUser);
    },
    async setRole(id: string, role: User["role"]): Promise<User> {
      const data = await request(
        `/api/admin/users/${encodeURIComponent(id)}/role`,
        { role },
      );
      if (!object(data))
        throw new ApiError("Invalid role response.", "INVALID_RESPONSE", false);
      return readUser(data.user);
    },
    async login(username: string, password: string) {
      return session(await request("/api/auth/login", { username, password }));
    },
    async me() {
      return session(await request("/api/auth/me"));
    },
    async logout() {
      await request("/api/auth/logout", {});
      csrf = "";
    },
    async askPolicy(question: string): Promise<PolicyAnswer> {
      const data = await request("/api/assistant/ask", { question });
      if (
        !object(data) ||
        typeof data.configured !== "boolean" ||
        typeof data.answer !== "string" ||
        !Array.isArray(data.sources) ||
        !data.sources.every(
          (source) =>
            object(source) &&
            typeof source.document === "string" &&
            typeof source.section === "string" &&
            typeof source.quote === "string" &&
            Number.isSafeInteger(source.chunk_index) &&
            Number(source.chunk_index) >= 0,
        )
      ) {
        throw new ApiError(
          "The API returned an invalid policy answer.",
          "INVALID_RESPONSE",
          false,
        );
      }
      return data as unknown as PolicyAnswer;
    },
    async list(): Promise<Purchase[]> {
      const data = await request("/api/purchases");
      if (!object(data) || !Array.isArray(data.purchases))
        throw new ApiError(
          "The API returned an unexpected purchase list.",
          "INVALID_RESPONSE",
          false,
        );
      return data.purchases.map(readPurchase);
    },
    async create(input: CreatePurchase): Promise<Purchase> {
      return readPurchase(await request("/api/purchases", input));
    },
    async decide(id: string, input: DecisionInput): Promise<DecisionResult> {
      const data = await request(
        `/api/purchases/${encodeURIComponent(id)}/decision`,
        input,
      );
      if (
        !object(data) ||
        data.purchase_id !== id ||
        data.status !== input.decision ||
        data.demo_only !== false ||
        data.metadata_persisted !== false
      )
        throw new ApiError(
          "The API returned an unexpected decision response. Refresh purchases to check the saved state.",
          "INVALID_RESPONSE",
          false,
        );
      return data as unknown as DecisionResult;
    },
  };
}

export const api = createApiClient(
  import.meta.env.VITE_API_BASE_URL || "http://localhost:8080",
);
export type PurchaseApi = ReturnType<typeof createApiClient>;
