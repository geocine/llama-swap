import type { ChatMessage, ContentPart } from "./types";
import { getTextContent } from "./types";

export const COMPACT_KEEP_RECENT_MESSAGES = 6;
const COMPACT_MAX_MESSAGE_CHARS = 12_000;
const COMPACT_MAX_INPUT_TOKENS_WITHOUT_CONTEXT = 24_000;
const CHARS_PER_TOKEN_ESTIMATE = 4;

export interface CompactionPlan {
  compactFromIndex: number;
  insertAtIndex: number;
  messagesToSummarize: ChatMessage[];
  messagesToKeep: ChatMessage[];
  droppedMessageCount: number;
  estimatedInputTokens: number;
}

export function isCompactSummaryMessage(message: ChatMessage): boolean {
  return message.role === "system" && message.compaction?.kind === "summary";
}

export function lastCompactSummaryIndex(messages: ChatMessage[]): number {
  for (let i = messages.length - 1; i >= 0; i -= 1) {
    if (isCompactSummaryMessage(messages[i])) return i;
  }
  return -1;
}

export function messagesForNextTurn(messages: ChatMessage[]): ChatMessage[] {
  const boundaryIndex = lastCompactSummaryIndex(messages);
  return boundaryIndex >= 0 ? messages.slice(boundaryIndex) : messages;
}

export function estimateTokens(text: string): number {
  return Math.ceil(text.length / CHARS_PER_TOKEN_ESTIMATE);
}

function compactText(text: string): string {
  if (text.length <= COMPACT_MAX_MESSAGE_CHARS) return text;
  return `${text.slice(0, COMPACT_MAX_MESSAGE_CHARS)}\n\n[message truncated for compaction]`;
}

function contentForCompaction(content: string | ContentPart[]): string {
  if (typeof content === "string") {
    return compactText(content);
  }

  const text = getTextContent(content);
  const imageCount = content.filter((part) => part.type === "image_url").length;
  const imageNote = imageCount > 0 ? `\n[${imageCount} image${imageCount === 1 ? "" : "s"} omitted during compaction]` : "";
  return compactText(`${text}${imageNote}`.trim());
}

function serializeMessage(message: ChatMessage, index: number): string {
  const role = message.role.toUpperCase();
  const reasoning = message.reasoning_content
    ? `\n\nREASONING:\n${compactText(message.reasoning_content)}`
    : "";
  return `[${index + 1}] ${role}:\n${contentForCompaction(message.content)}${reasoning}`;
}

function estimateMessagesTokens(messages: ChatMessage[]): number {
  return estimateTokens(messages.map((message, index) => serializeMessage(message, index)).join("\n\n"));
}

export function planConversationCompaction(
  messages: ChatMessage[],
  contextSize = 0,
  keepRecentMessages = COMPACT_KEEP_RECENT_MESSAGES
): CompactionPlan | null {
  const compactFromIndex = lastCompactSummaryIndex(messages) + 1;
  const compactableMessages = messages.slice(compactFromIndex);
  if (compactableMessages.length <= keepRecentMessages + 1) return null;

  const keepCount = Math.min(keepRecentMessages, Math.max(2, compactableMessages.length - 2));
  const messagesToKeep = compactableMessages.slice(-keepCount);
  let messagesToSummarize = compactableMessages.slice(0, -keepCount);
  let droppedMessageCount = 0;
  const targetTokens =
    contextSize > 0 ? Math.max(2048, Math.floor(contextSize * 0.55)) : COMPACT_MAX_INPUT_TOKENS_WITHOUT_CONTEXT;

  while (messagesToSummarize.length > 1 && estimateMessagesTokens(messagesToSummarize) > targetTokens) {
    messagesToSummarize = messagesToSummarize.slice(1);
    droppedMessageCount += 1;
  }

  if (messagesToSummarize.length === 0) return null;

  return {
    compactFromIndex,
    insertAtIndex: messages.length - keepCount,
    messagesToSummarize,
    messagesToKeep,
    droppedMessageCount,
    estimatedInputTokens: estimateMessagesTokens(messagesToSummarize),
  };
}

export function buildCompactionRequestMessages(plan: CompactionPlan): ChatMessage[] {
  const droppedNote =
    plan.droppedMessageCount > 0
      ? `\n\n${plan.droppedMessageCount} earliest message(s) were omitted because the compaction request was too large.`
      : "";

  return [
    {
      role: "system",
      content:
        "You summarize chat history for context compaction. Respond with a concise but complete summary. Do not call tools. Do not include analysis tags.",
    },
    {
      role: "user",
      content: `Summarize the older part of this conversation so another assistant can continue from the preserved recent messages.${droppedNote}

Include:
- user goals and preferences
- technical decisions and constraints
- files or commands discussed
- errors, fixes, and current unresolved work

Conversation to summarize:

${plan.messagesToSummarize.map(serializeMessage).join("\n\n")}`,
    },
  ];
}

export function buildCompactSummaryMessage(plan: CompactionPlan, summary: string): ChatMessage {
  return {
    role: "system",
    content: `Earlier conversation compacted. Use this summary as context and continue with the preserved recent messages.

${summary.trim()}`,
    compaction: {
      kind: "summary",
      createdAt: Date.now(),
      summarizedMessageCount: plan.messagesToSummarize.length,
      droppedMessageCount: plan.droppedMessageCount,
    },
  };
}

export function insertCompactSummary(messages: ChatMessage[], plan: CompactionPlan, summary: string): ChatMessage[] {
  return [
    ...messages.slice(0, plan.insertAtIndex),
    buildCompactSummaryMessage(plan, summary),
    ...messages.slice(plan.insertAtIndex),
  ];
}
