import { describe, expect, it } from "vitest";
import { formatElapsed, getChatModelLoadingState, resolveSelectedModel } from "./modelLoading";
import type { Model } from "./types";

const models: Model[] = [
  {
    id: "qwen35-27b",
    state: "starting",
    name: "qwen35-27b",
    description: "",
    unlisted: false,
    peerID: "",
    aliases: ["qwen"],
  },
  {
    id: "ready-model",
    state: "ready",
    name: "ready-model",
    description: "",
    unlisted: false,
    peerID: "",
  },
];

describe("model loading helpers", () => {
  it("resolves selected models by id or alias", () => {
    expect(resolveSelectedModel(models, "qwen35-27b")?.id).toBe("qwen35-27b");
    expect(resolveSelectedModel(models, "qwen")?.id).toBe("qwen35-27b");
    expect(resolveSelectedModel(models, "missing")).toBeUndefined();
  });

  it("returns a loading state while a non-ready model has not streamed output", () => {
    expect(getChatModelLoadingState(models, "qwen", true, false, 1000, 4500)).toEqual({
      modelId: "qwen35-27b",
      state: "starting",
      label: "Loading model",
      elapsedMs: 3500,
      downloadProgress: null,
    });
  });

  it("uses download progress as the loading label when active", () => {
    expect(
      getChatModelLoadingState(models, "qwen", true, false, 1000, 4500, {
        active: true,
        filename: "model.gguf.downloadInProgress",
        downloadedBytes: "1.00 GB",
        totalBytes: "2.00 GB",
        percent: 50,
        message: "Downloading model weights",
      })
    ).toMatchObject({
      modelId: "qwen35-27b",
      state: "starting",
      label: "Downloading model weights",
      downloadProgress: {
        active: true,
        percent: 50,
      },
    });
  });

  it("does not report loading after output starts or the model is ready", () => {
    expect(getChatModelLoadingState(models, "qwen", true, true, 1000, 4500)).toBeNull();
    expect(getChatModelLoadingState(models, "ready-model", true, false, 1000, 4500)).toBeNull();
  });

  it("formats elapsed time for long starts", () => {
    expect(formatElapsed(900)).toBe("0s");
    expect(formatElapsed(65000)).toBe("1m 05s");
  });
});
