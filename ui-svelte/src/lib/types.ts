export type ConnectionState = "connected" | "connecting" | "disconnected";

export type ModelStatus = "ready" | "starting" | "stopping" | "stopped" | "shutdown" | "unknown";

export interface Model {
  id: string;
  state: ModelStatus;
  stateChangedAt?: string;
  name: string;
  description: string;
  unlisted: boolean;
  peerID: string;
  aliases?: string[];
  contextSize?: number;
}

export interface Metrics {
  id: number;
  timestamp: string;
  model: string;
  cache_tokens: number;
  input_tokens: number;
  output_tokens: number;
  prompt_per_second: number;
  tokens_per_second: number;
  duration_ms: number;
  has_capture: boolean;
}

export interface ReqRespCapture {
  id: number;
  req_path: string;
  req_headers: Record<string, string>;
  req_body: string; // base64 encoded bytes
  resp_headers: Record<string, string>;
  resp_body: string; // base64 encoded bytes
}

export interface LogData {
  source: "upstream" | "proxy";
  data: string;
}

export interface InFlightStats {
  total: number;
}

export interface APIEventEnvelope {
  type: "modelStatus" | "logData" | "metrics" | "inflight" | "activity";
  data: string;
}

export interface VersionInfo {
  build_date: string;
  commit: string;
  version: string;
}

export interface ServerInfo {
  authRequired: boolean;
  apiKey?: string;
}

export interface ModelDownloadProgress {
  active: boolean;
  filename?: string;
  downloadedBytes?: string;
  totalBytes?: string;
  percent?: number;
  message: string;
}

export interface SessionModelSettings {
  alias: string;
  source: string;
  serverArgs: string;
  kvCacheArgs: string;
  samplingArgs: string;
  grammarArgs: string;
}

export interface EditableModelConfig {
  modelId: string;
  state: ModelStatus;
  base: SessionModelSettings;
  override?: SessionModelSettings;
  effective: SessionModelSettings;
  editable: boolean;
  message?: string;
  command: string;
  userAdded: boolean;
  sourceModelId?: string;
}

export interface ConfigImportResult {
  imported: string[];
  skipped: string[];
}

export type ScreenWidth = "xs" | "sm" | "md" | "lg" | "xl" | "2xl";

export type TextContentPart = {
  type: "text";
  text: string;
};

export type ImageContentPart = {
  type: "image_url";
  image_url: { url: string };
};

export type ContentPart = TextContentPart | ImageContentPart;

export interface ChatMessageTimings {
  prompt_n?: number;
  prompt_ms?: number;
  prompt_per_second?: number;
  predicted_n?: number;
  predicted_ms?: number;
  predicted_per_second?: number;
  cache_n?: number;
}

export interface ChatMessagePromptProgress {
  total: number;
  cache: number;
  processed: number;
  time_ms: number;
}

export interface ChatMessage {
  role: "user" | "assistant" | "system";
  content: string | ContentPart[];
  reasoning_content?: string;
  reasoningTimeMs?: number;
  timings?: ChatMessageTimings;
  promptProgress?: ChatMessagePromptProgress;
  compaction?: {
    kind: "summary";
    createdAt: number;
    summarizedMessageCount: number;
    droppedMessageCount: number;
  };
}

export function getTextContent(content: string | ContentPart[]): string {
  if (typeof content === "string") {
    return content;
  }
  const textParts = content.filter((part): part is TextContentPart => part.type === "text");
  return textParts.map((part) => part.text).join("\n");
}

function isDisplayableImageUrl(url: string): boolean {
  const trimmed = url.trim();
  return trimmed.startsWith("data:image/") || trimmed.startsWith("blob:") || trimmed.startsWith("http://") || trimmed.startsWith("https://");
}

export function getImageUrls(content: string | ContentPart[]): string[] {
  if (typeof content === "string") {
    return [];
  }
  return content
    .filter((part): part is ImageContentPart => part.type === "image_url")
    .map((part) => part.image_url.url)
    .filter(isDisplayableImageUrl);
}

export interface ChatCompletionRequest {
  model: string;
  messages: ChatMessage[];
  stream: boolean;
  temperature?: number;
  max_tokens?: number;
  return_progress?: boolean;
  timings_per_token?: boolean;
}

export interface ImageGenerationRequest {
  model: string;
  prompt: string;
  n?: number;
  size?: string;
}

export interface ImageGenerationResponse {
  created: number;
  data: Array<{
    url?: string;
    b64_json?: string;
  }>;
}

export interface AudioTranscriptionRequest {
  file: File;
  model: string;
}

export interface AudioTranscriptionResponse {
  text: string;
}

export interface SpeechGenerationRequest {
  model: string;
  input: string;
  voice: string;
}
