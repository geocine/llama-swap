import type { Model, ModelStatus } from "./types";

export interface ChatModelLoadingState {
  modelId: string;
  state: ModelStatus;
  label: string;
  elapsedMs: number;
}

export function resolveSelectedModel(models: Model[], selectedModel: string): Model | undefined {
  if (!selectedModel) {
    return undefined;
  }

  return models.find((model) => model.id === selectedModel || model.aliases?.includes(selectedModel));
}

export function formatElapsed(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  if (seconds < 60) {
    return `${seconds}s`;
  }

  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return `${minutes}m ${remainingSeconds.toString().padStart(2, "0")}s`;
}

export function getChatModelLoadingState(
  models: Model[],
  selectedModel: string,
  isStreaming: boolean,
  hasReceivedOutput: boolean,
  requestStartedAt: number,
  now: number
): ChatModelLoadingState | null {
  if (!isStreaming || hasReceivedOutput || !selectedModel || requestStartedAt <= 0) {
    return null;
  }

  const model = resolveSelectedModel(models, selectedModel);
  if (!model) {
    return {
      modelId: selectedModel,
      state: "unknown",
      label: "Waiting for model status",
      elapsedMs: now - requestStartedAt,
    };
  }

  if (model.state === "ready") {
    return null;
  }

  const labels: Record<ModelStatus, string> = {
    ready: "Ready",
    starting: "Loading model",
    stopping: "Stopping previous process",
    stopped: "Starting model",
    shutdown: "Model process is shut down",
    unknown: "Waiting for model status",
  };

  return {
    modelId: model.id,
    state: model.state,
    label: labels[model.state],
    elapsedMs: now - requestStartedAt,
  };
}
