import { afterEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "./api";

const purchase = {
  id: "request-1",
  client_request_id: "client-1",
  employee_id: "emp-101",
  item_name: "Laptop",
  quantity: 2,
  unit_price_cents: 80000,
  total_cents: 160000,
  status: "PENDING",
};
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("typed gateway client", () => {
  it("uses the configured URL, exact integers, and no credentials", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify(purchase), { status: 201 }),
      );
    vi.stubGlobal("fetch", fetcher);
    const input = {
      client_request_id: "client-1",
      employee_id: "emp-101",
      item_name: "Laptop",
      quantity: 2,
      unit_price_cents: 80000,
    };
    expect(
      (await createApiClient("http://localhost:8080/").create(input))
        .total_cents,
    ).toBe(160000);
    expect(fetcher.mock.calls[0][0]).toBe(
      "http://localhost:8080/api/purchases",
    );
    expect(fetcher.mock.calls[0][1].credentials).toBe("omit");
    expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual(input);
  });
  it("reads real lists without inventing dates and rejects unsafe numbers", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ purchases: [purchase] })),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            purchases: [{ ...purchase, total_cents: 9007199254740992 }],
          }),
        ),
      );
    vi.stubGlobal("fetch", fetcher);
    const client = createApiClient("http://localhost:8080");
    expect((await client.list())[0].created_at).toBeUndefined();
    await expect(client.list()).rejects.toMatchObject({
      code: "UNSAFE_NUMBER",
    });
  });
  it("preserves controlled error and retry semantics", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({
              error: {
                code: "UNAVAILABLE",
                message: "Retry the same request.",
                retryable: true,
              },
            }),
            { status: 503 },
          ),
        ),
    );
    await expect(
      createApiClient("http://localhost:8080").list(),
    ).rejects.toMatchObject({
      code: "UNAVAILABLE",
      retryable: true,
      status: 503,
    });
  });
  it("reports network failures and validates base configuration without a request", async () => {
    const fetcher = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
    vi.stubGlobal("fetch", fetcher);
    await expect(
      createApiClient("http://localhost:8080").list(),
    ).rejects.toMatchObject({ code: "NETWORK", retryable: true });
    await expect(
      createApiClient("http://user:secret@localhost:8080").list(),
    ).rejects.toMatchObject({ code: "CONFIGURATION" });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("aborts timed-out requests", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        (_url, options) =>
          new Promise((_resolve, reject) => {
            options.signal.addEventListener("abort", () =>
              reject(new DOMException("Aborted", "AbortError")),
            );
          }),
      ),
    );
    await expect(
      createApiClient("http://localhost:8080", 5).list(),
    ).rejects.toMatchObject({ code: "TIMEOUT", retryable: true });
  });
  it("sends manual decisions to the gateway with explicit demo metadata", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({
            purchase_id: "request-1",
            status: "APPROVED",
            demo_only: true,
            metadata_persisted: false,
          }),
        ),
      );
    vi.stubGlobal("fetch", fetcher);
    await createApiClient("http://localhost:8080").decide("request-1", {
      manager_id: "manager-demo",
      decision: "APPROVED",
      comment: "Within budget",
    });
    expect(fetcher.mock.calls[0][0]).toContain(
      "/api/purchases/request-1/decision",
    );
    expect(JSON.parse(fetcher.mock.calls[0][1].body).manager_id).toBe(
      "manager-demo",
    );
  });
});
