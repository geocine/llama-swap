import { describe, expect, it } from "vitest";
import {
  buildCompactionRequestMessages,
  insertCompactSummary,
  messagesForNextTurn,
  planConversationCompaction,
} from "./compaction";
import type { ChatMessage } from "./types";

function message(role: "user" | "assistant", content: string): ChatMessage {
  return { role, content };
}

describe("conversation compaction helpers", () => {
  it("preserves recent messages and summarizes the older prefix", () => {
    const messages = Array.from({ length: 10 }, (_, index) =>
      message(index % 2 === 0 ? "user" : "assistant", `message ${index}`)
    );

    const plan = planConversationCompaction(messages, 32000, 4);

    expect(plan).not.toBeNull();
    expect(plan?.messagesToSummarize).toEqual(messages.slice(0, 6));
    expect(plan?.messagesToKeep).toEqual(messages.slice(-4));
  });

  it("caps oversized compaction input by dropping from the head", () => {
    const messages = Array.from({ length: 12 }, (_, index) =>
      message(index % 2 === 0 ? "user" : "assistant", "x".repeat(2000 + index))
    );

    const plan = planConversationCompaction(messages, 4096, 4);

    expect(plan).not.toBeNull();
    expect(plan!.droppedMessageCount).toBeGreaterThan(0);
    expect(plan!.messagesToKeep).toEqual(messages.slice(-4));
  });

  it("inserts a compact summary before the preserved tail without removing history", () => {
    const messages = Array.from({ length: 8 }, (_, index) =>
      message(index % 2 === 0 ? "user" : "assistant", `message ${index}`)
    );
    const plan = planConversationCompaction(messages, 32000, 4)!;

    const compacted = insertCompactSummary(messages, plan, "Important summary");

    expect(compacted).toHaveLength(messages.length + 1);
    expect(compacted.slice(0, 4)).toEqual(messages.slice(0, 4));
    expect(compacted[4]).toEqual({
      role: "system",
      content: expect.stringContaining("Important summary"),
      compaction: {
        kind: "summary",
        createdAt: expect.any(Number),
        summarizedMessageCount: 4,
        droppedMessageCount: 0,
      },
    });
    expect(compacted.slice(5)).toEqual(messages.slice(-4));
  });

  it("uses only the latest compact summary and later messages for future turns", () => {
    const messages = Array.from({ length: 8 }, (_, index) =>
      message(index % 2 === 0 ? "user" : "assistant", `message ${index}`)
    );
    const firstPlan = planConversationCompaction(messages, 32000, 4)!;
    const compacted = insertCompactSummary(messages, firstPlan, "Important summary");

    expect(messagesForNextTurn(compacted)).toEqual(compacted.slice(4));
  });

  it("strips image data from the summarization request", () => {
    const messages: ChatMessage[] = [
      {
        role: "user",
        content: [
          { type: "text", text: "look at this" },
          { type: "image_url", image_url: { url: "data:image/png;base64," + "x".repeat(10_000) } },
        ],
      },
      message("assistant", "ok"),
      message("user", "next"),
      message("assistant", "done"),
      message("user", "tail"),
      message("assistant", "tail response"),
    ];
    const plan = planConversationCompaction(messages, 32000, 2)!;

    const requestMessages = buildCompactionRequestMessages(plan);
    const requestText = requestMessages.map((requestMessage) => requestMessage.content).join("\n");

    expect(requestText).toContain("[1 image omitted during compaction]");
    expect(requestText).not.toContain("data:image/png;base64");
  });
});
