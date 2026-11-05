import { createApp, nextTick } from "vue";
import { afterEach, expect, it, vi } from "vitest";
import PolicyAssistant from "./PolicyAssistant.vue";
import { api } from "../lib/api";

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

it("stays idle until asked and shows Not configured without affecting purchases", async () => {
  const ask = vi
    .spyOn(api, "askPolicy")
    .mockResolvedValue({
      configured: false,
      answer: "Not configured",
      sources: [],
    });
  const root = document.createElement("div");
  document.body.append(root);
  const app = createApp(PolicyAssistant);
  app.mount(root);
  try {
    expect(ask).not.toHaveBeenCalled();
    const textarea = root.querySelector("textarea")!;
    textarea.value = "What is the fictional policy?";
    textarea.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
    root
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await vi.waitFor(() =>
      expect(root.textContent).toContain("Not configured"),
    );
    expect(ask).toHaveBeenCalledExactlyOnceWith(
      "What is the fictional policy?",
    );
    expect(
      root.querySelector("[aria-label='Retrieved policy sources']"),
    ).toBeNull();
  } finally {
    app.unmount();
  }
});

it("renders evidence as plain text and keeps provider failures visible", async () => {
  const ask = vi
    .spyOn(api, "askPolicy")
    .mockResolvedValue({
      configured: true,
      answer: "Fictional policy answer.",
      sources: [
        {
          document: "fictional.md",
          section: "Rule@L1",
          chunk_index: 0,
          quote: "<script>untrusted text</script>",
        },
      ],
    });
  const root = document.createElement("div");
  document.body.append(root);
  const app = createApp(PolicyAssistant);
  app.mount(root);
  try {
    const textarea = root.querySelector("textarea")!;
    textarea.value = "What is the fictional policy?";
    textarea.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
    const submit = () =>
      root
        .querySelector("form")!
        .dispatchEvent(
          new Event("submit", { bubbles: true, cancelable: true }),
        );
    submit();
    await vi.waitFor(() => expect(root.textContent).toContain("fictional.md"));
    expect(root.querySelector("blockquote")!.textContent).toBe(
      "<script>untrusted text</script>",
    );
    expect(root.querySelector("script")).toBeNull();
    ask.mockRejectedValueOnce(new Error("Policy assistant unavailable"));
    submit();
    await vi.waitFor(() =>
      expect(root.querySelector("[role=alert]")?.textContent).toContain(
        "unavailable",
      ),
    );
    expect(root.querySelector("blockquote")).toBeNull();
  } finally {
    app.unmount();
  }
});
