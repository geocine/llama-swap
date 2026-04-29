import type { ChatMessage, ChatCompletionRequest } from "./types";

// Mirrors the `timings` block emitted by llama.cpp's OpenAI-compatible
// chat completion endpoint. Fields can arrive incrementally during streaming
// and are finalized on the last chunk before [DONE].
export interface ChatTimings {
  prompt_n?: number;
  prompt_ms?: number;
  prompt_per_second?: number;
  predicted_n?: number;
  predicted_ms?: number;
  predicted_per_second?: number;
  cache_n?: number;
}

export interface StreamChunk {
  content: string;
  reasoning_content?: string;
  timings?: ChatTimings;
  done: boolean;
}

export interface ChatOptions {
  temperature?: number;
}

function parseSSELine(line: string): StreamChunk | null {
  const trimmed = line.trim();
  if (!trimmed || !trimmed.startsWith("data: ")) {
    return null;
  }

  const data = trimmed.slice(6);
  if (data === "[DONE]") {
    return { content: "", done: true };
  }

  try {
    const parsed = JSON.parse(data);
    const delta = parsed.choices?.[0]?.delta;
    const content = delta?.content || "";
    const reasoning_content = delta?.reasoning_content || "";
    // llama.cpp emits a top-level `timings` object on each chunk
    const timings = parsed.timings as ChatTimings | undefined;

    if (content || reasoning_content || timings) {
      return { content, reasoning_content, timings, done: false };
    }
    return null;
  } catch {
    return null;
  }
}

export async function* streamChatCompletion(
  model: string,
  messages: ChatMessage[],
  signal?: AbortSignal,
  options?: ChatOptions
): AsyncGenerator<StreamChunk> {
  const request: ChatCompletionRequest = {
    model,
    messages,
    stream: true,
    temperature: options?.temperature,
  };

  const response = await fetch("/v1/chat/completions", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(request),
    signal,
  });

  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(`Chat API error: ${response.status} - ${errorText}`);
  }

  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error("Response body is not readable");
  }

  const decoder = new TextDecoder();
  let buffer = "";

  try {
    while (true) {
      const { done, value } = await reader.read();

      if (done) {
        break;
      }

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() || "";

      for (const line of lines) {
        const result = parseSSELine(line);
        if (result?.done) {
          yield result;
          return;
        }
        if (result) {
          yield result;
        }
      }
    }

    // Process any remaining buffer
    const result = parseSSELine(buffer);
    if (result && !result.done) {
      yield result;
    }

    yield { content: "", done: true };
  } finally {
    reader.releaseLock();
  }
}
