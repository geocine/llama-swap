import { afterEach, describe, expect, it, vi } from "vitest";
import { streamChatCompletion } from "./chatApi";

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
});
