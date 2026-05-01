import { afterEach, describe, expect, it, vi } from "vitest";
import { completeChatCompletion, streamChatCompletion } from "./chatApi";

function streamFromText(text: string): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode(text));
      controller.close();
    },
  });
}

describe("streamChatCompletion", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("requests prompt progress and yields prompt progress chunks", async () => {
    const body = [
      'data: {"prompt_progress":{"total":100,"cache":10,"processed":55,"time_ms":1500}}',
      'data: {"choices":[{"delta":{"content":"ok"}}],"timings":{"prompt_n":100,"predicted_n":1,"predicted_ms":25}}',
      "data: [DONE]",
      "",
    ].join("\n");
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(streamFromText(body), {
        status: 200,
        headers: { "Content-Type": "text/event-stream" },
      })
    );
    vi.stubGlobal("fetch", fetchMock);

    const chunks = [];
    for await (const chunk of streamChatCompletion("model-a", [{ role: "user", content: "hello" }])) {
      chunks.push(chunk);
    }

    const request = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(request.return_progress).toBe(true);
    expect(request.timings_per_token).toBe(true);
    expect(chunks[0]).toMatchObject({
      prompt_progress: { total: 100, cache: 10, processed: 55, time_ms: 1500 },
    });
    expect(chunks[1]).toMatchObject({
      content: "ok",
      timings: { prompt_n: 100, predicted_n: 1, predicted_ms: 25 },
    });
  });

  it("does not send empty or internal image urls upstream", async () => {
    const body = ["data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}", "data: [DONE]", ""].join("\n");
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(streamFromText(body), {
        status: 200,
        headers: { "Content-Type": "text/event-stream" },
      })
    );
    vi.stubGlobal("fetch", fetchMock);

    const messages = [
      {
        role: "user" as const,
        content: [
          { type: "text" as const, text: "look" },
          { type: "image_url" as const, image_url: { url: "" } },
          { type: "image_url" as const, image_url: { url: "indexeddb://image/abc" } },
          { type: "image_url" as const, image_url: { url: "data:image/png;base64,abc" } },
        ],
      },
    ];

    for await (const _chunk of streamChatCompletion("model-a", messages)) {
      // drain stream
    }

    const request = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(request.messages[0].content).toEqual([
      { type: "text", text: "look" },
      { type: "image_url", image_url: { url: "data:image/png;base64,abc" } },
    ]);
  });

  it("sends text content when every image url is invalid", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      Response.json({
        choices: [{ message: { content: "ok" } }],
      })
    );
    vi.stubGlobal("fetch", fetchMock);

    await completeChatCompletion("model-a", [
      {
        role: "user",
        content: [
          { type: "image_url", image_url: { url: "" } },
          { type: "image_url", image_url: { url: "indexeddb://image/abc" } },
        ],
      },
    ]);

    const request = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(request.messages[0].content).toBe("");
  });
});
