import { writable } from "svelte/store";
import type {
  Model,
  Metrics,
  VersionInfo,
  LogData,
  APIEventEnvelope,
  ReqRespCapture,
  InFlightStats,
  ModelDownloadProgress,
} from "../lib/types";
import { handleUnauthorized } from "./auth";
import { connectionState } from "./theme";

const LOG_LENGTH_LIMIT = 1024 * 100; /* 100KB of log data */

// Stores
export const models = writable<Model[]>([]);
export const proxyLogs = writable<string>("");
export const upstreamLogs = writable<string>("");
export const metrics = writable<Metrics[]>([]);
export const inFlightRequests = writable<number>(0);
export const downloadProgress = writable<ModelDownloadProgress | null>(null);
export const versionInfo = writable<VersionInfo>({
  build_date: "unknown",
  commit: "unknown",
  version: "unknown",
});

let apiEventSource: EventSource | null = null;

async function apiFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const response = await fetch(input, init);
  if (response.status === 401) {
    handleUnauthorized();
  }
  return response;
}

function appendLog(newData: string, store: typeof proxyLogs | typeof upstreamLogs): void {
  store.update((prev) => {
    const updatedLog = prev + newData;
    return updatedLog.length > LOG_LENGTH_LIMIT ? updatedLog.slice(-LOG_LENGTH_LIMIT) : updatedLog;
  });
}

function parseDownloadProgressLine(line: string): ModelDownloadProgress | null {
  const modelTotal = line.match(
    /^llama-server-progress: model download (.+) \(([^/]+) \/ ([^,]+), ([\d.]+)%\)$/
  );
  if (modelTotal) {
    return {
      active: true,
      filename: modelTotal[1],
      downloadedBytes: modelTotal[2].trim(),
      totalBytes: modelTotal[3].trim(),
      percent: Number(modelTotal[4]),
      message: "Downloading model weights",
    };
  }

  const withTotal = line.match(
    /^llama-server-progress: downloading (.+) \(([^/]+) \/ ([^,]+), ([\d.]+)%\)$/
  );
  if (withTotal) {
    return {
      active: true,
      filename: withTotal[1],
      downloadedBytes: withTotal[2].trim(),
      totalBytes: withTotal[3].trim(),
      percent: Number(withTotal[4]),
      message: "Downloading model file",
    };
  }

  const withoutTotal = line.match(/^llama-server-progress: downloading (.+) \(([^)]+)\)$/);
  if (withoutTotal) {
    return {
      active: true,
      filename: withoutTotal[1],
      downloadedBytes: withoutTotal[2].trim(),
      message: "Downloading model file",
    };
  }

  if (line.includes("llama-server-progress: download finished")) {
    return {
      active: false,
      message: "Download complete, initializing model",
    };
  }

  return null;
}

function updateDownloadProgress(logData: string): void {
  for (const line of logData.split(/\r?\n/)) {
    const progress = parseDownloadProgressLine(line.trim());
    if (progress) {
      downloadProgress.set(progress);
    }
  }
}

export function enableAPIEvents(enabled: boolean): void {
  if (!enabled) {
    apiEventSource?.close();
    apiEventSource = null;
    metrics.set([]);
    inFlightRequests.set(0);
    downloadProgress.set(null);
    return;
  }

  let retryCount = 0;
  const initialDelay = 1000; // 1 second

  const connect = () => {
    apiEventSource?.close();
    apiEventSource = new EventSource("/api/events");

    connectionState.set("connecting");

    apiEventSource.onopen = () => {
      // Clear everything on connect to keep things in sync
      proxyLogs.set("");
      upstreamLogs.set("");
      metrics.set([]);
      inFlightRequests.set(0);
      downloadProgress.set(null);
      models.set([]);
      retryCount = 0;
      connectionState.set("connected");
    };

    apiEventSource.onmessage = (e: MessageEvent) => {
      try {
        const message = JSON.parse(e.data) as APIEventEnvelope;
        switch (message.type) {
          case "modelStatus": {
            const newModels = JSON.parse(message.data) as Model[];
            // Sort models by name and id
            newModels.sort((a, b) => {
              return (a.name + a.id).localeCompare(b.name + b.id);
            });
            models.set(newModels);
            break;
          }

          case "logData": {
            const logData = JSON.parse(message.data) as LogData;
            switch (logData.source) {
              case "proxy":
                appendLog(logData.data, proxyLogs);
                break;
              case "upstream":
                appendLog(logData.data, upstreamLogs);
                updateDownloadProgress(logData.data);
                break;
            }
            break;
          }

          case "metrics": {
            const newMetrics = JSON.parse(message.data) as Metrics[];
            metrics.update((prevMetrics) => [...newMetrics, ...prevMetrics]);
            break;
          }
          case "inflight": {
            const stats = JSON.parse(message.data) as InFlightStats;
            inFlightRequests.set(stats.total ?? 0);
            break;
          }
          case "activity": {
            metrics.set([]);
            break;
          }
        }
      } catch (err) {
        console.error(e.data, err);
      }
    };

    apiEventSource.onerror = () => {
      apiEventSource?.close();
      retryCount++;
      const delay = Math.min(initialDelay * Math.pow(2, retryCount - 1), 5000);
      connectionState.set("disconnected");
      setTimeout(connect, delay);
    };
  };

  connect();
}

// Fetch version info when connected
connectionState.subscribe(async (status) => {
  if (status === "connected") {
    try {
      const response = await apiFetch("/api/version");
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      const data: VersionInfo = await response.json();
      versionInfo.set(data);
    } catch (error) {
      console.error(error);
    }
  }
});

export async function listModels(): Promise<Model[]> {
  try {
    const response = await apiFetch("/api/models/");
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    const data = await response.json();
    return data || [];
  } catch (error) {
    console.error("Failed to fetch models:", error);
    return [];
  }
}

export async function unloadAllModels(): Promise<void> {
  try {
    const response = await apiFetch(`/api/models/unload`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to unload models: ${response.status}`);
    }
  } catch (error) {
    console.error("Failed to unload models:", error);
    throw error;
  }
}

export async function unloadSingleModel(model: string): Promise<void> {
  try {
    const response = await apiFetch(`/api/models/unload/${model}`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to unload model: ${response.status}`);
    }
  } catch (error) {
    console.error("Failed to unload model", model, error);
    throw error;
  }
}

export async function loadModel(model: string): Promise<void> {
  try {
    const response = await apiFetch(`/upstream/${model}/`, {
      method: "GET",
    });
    if (!response.ok) {
      throw new Error(`Failed to load model: ${response.status}`);
    }
  } catch (error) {
    console.error("Failed to load model:", error);
    throw error;
  }
}

export async function getCapture(id: number): Promise<ReqRespCapture | null> {
  try {
    const response = await apiFetch(`/api/captures/${id}`);
    if (response.status === 404) {
      return null;
    }
    if (!response.ok) {
      throw new Error(`Failed to fetch capture: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error("Failed to fetch capture:", error);
    return null;
  }
}

export async function downloadActivityDB(): Promise<void> {
  try {
    const response = await apiFetch("/api/metrics/export");
    if (!response.ok) {
      throw new Error(`Failed to export activity database: ${response.status}`);
    }
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "llama-swap-activity.sqlite";
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  } catch (error) {
    console.error("Failed to export activity database:", error);
    throw error;
  }
}

export async function clearActivity(): Promise<void> {
  try {
    const response = await apiFetch("/api/metrics", { method: "DELETE" });
    if (!response.ok) {
      throw new Error(`Failed to clear activity: ${response.status}`);
    }
    metrics.set([]);
  } catch (error) {
    console.error("Failed to clear activity:", error);
    throw error;
  }
}
