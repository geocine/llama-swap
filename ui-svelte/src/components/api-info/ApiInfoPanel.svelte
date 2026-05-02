<script lang="ts">
  import { models, serverInfo } from "../../stores/api";
  import CopyField from "./CopyField.svelte";
  import SnippetBlock from "./SnippetBlock.svelte";

  let appBaseURL = $state("");
  $effect(() => {
    if (typeof window !== "undefined") {
      appBaseURL = window.location.origin;
    }
  });
  let openAIBaseURL = $derived(appBaseURL ? `${appBaseURL}/v1` : "");

  type BaseUrlKind = "openai" | "anthropic";
  let baseUrlKind = $state<BaseUrlKind>("openai");
  let activeBaseURL = $derived(baseUrlKind === "openai" ? openAIBaseURL : appBaseURL);

  type GuideTab = "curl" | "python" | "claude" | "codex";
  let activeTab = $state<GuideTab>("curl");
  const tabs: Array<{ id: GuideTab; label: string }> = [
    { id: "curl", label: "curl" },
    { id: "python", label: "Python" },
    { id: "claude", label: "Claude" },
    { id: "codex", label: "Codex" },
  ];

  let readyModel = $derived($models.find((m) => m.state === "ready"));
  let loadingModel = $derived($models.find((m) => m.state === "starting"));
  // Prefer the live model (ready, then loading) so copied snippets always
  // target whatever the user just acted on. Fall back to the first listed
  // model so the example is never empty on a fresh server.
  let snippetModel = $derived(
    readyModel?.id ??
      loadingModel?.id ??
      $models.find((m) => !m.unlisted && !m.peerID)?.id ??
      $models[0]?.id ??
      "model-id"
  );
  let snippetModelState = $derived<"ready" | "loading" | "default">(
    readyModel ? "ready" : loadingModel ? "loading" : "default"
  );
  let haikuModel = $derived.by(() => {
    return (
      $models.find((m) => !m.peerID && /(haiku|mini|small|lite)/i.test(m.id))?.id ??
      snippetModel
    );
  });

  let apiKey = $derived($serverInfo?.apiKey ?? "");
  let authRequired = $derived($serverInfo?.authRequired ?? false);

  let curlSnippet = $derived(buildCurlSnippet(openAIBaseURL, apiKey, snippetModel));
  let streamingCurlSnippet = $derived(buildStreamingCurlSnippet(openAIBaseURL, apiKey, snippetModel));
  let pythonSnippet = $derived(buildPythonSnippet(openAIBaseURL, apiKey, snippetModel));
  let claudeShortcutSnippet = $derived(buildClaudeShortcutSnippet(appBaseURL, apiKey, snippetModel, haikuModel));
  let codexBashSnippet = $derived(buildCodexBashSnippet(apiKey));
  let codexTomlSnippet = $derived(buildCodexTomlSnippet(openAIBaseURL, snippetModel));

  function shellQuote(value: string): string {
    return `"${value.replace(/(["\\$`])/g, "\\$1")}"`;
  }

  function tomlQuote(value: string): string {
    return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
  }

  function buildCurlSnippet(url: string, key: string, model: string): string {
    const auth = key ? `\n  -H "Authorization: Bearer ${key}" \\` : "";
    return `curl ${url}/chat/completions \\${auth}
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${model}",
    "messages": [{"role": "user", "content": "Hello"}]
  }'`;
  }

  function buildStreamingCurlSnippet(url: string, key: string, model: string): string {
    const auth = key ? `\n  -H "Authorization: Bearer ${key}" \\` : "";
    return `curl -N ${url}/chat/completions \\${auth}
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${model}",
    "stream": true,
    "messages": [{"role": "user", "content": "Write a short hello"}]
  }'`;
  }

  function buildPythonSnippet(url: string, key: string, model: string): string {
    const apiKeyArg = key || "EMPTY";
    return `from openai import OpenAI

client = OpenAI(
    base_url="${url}",
    api_key="${apiKeyArg}",
)

response = client.chat.completions.create(
    model="${model}",
    messages=[{"role": "user", "content": "Hello"}],
)
print(response.choices[0].message.content)`;
  }

  function buildClaudeShortcutSnippet(url: string, key: string, primaryModel: string, smallModel: string): string {
    const token = key || "sk-dummy";
    return `cllama() {
  export ANTHROPIC_BASE_URL=${shellQuote(url)}
  export ANTHROPIC_AUTH_TOKEN=${shellQuote(token)}
  export ANTHROPIC_DEFAULT_OPUS_MODEL=${shellQuote(primaryModel)}
  export ANTHROPIC_DEFAULT_SONNET_MODEL=${shellQuote(primaryModel)}
  export ANTHROPIC_DEFAULT_HAIKU_MODEL=${shellQuote(smallModel)}
  claude
}`;
  }

  function buildCodexBashSnippet(key: string): string {
    const token = key || "EMPTY";
    return `cdllama() {
  export AI_AUTH_KEY=${shellQuote(token)}
  codex -p llama
}`;
  }

  function buildCodexTomlSnippet(url: string, model: string): string {
    return `[model_providers.llama_cpp]
name = "llama_cpp API"
base_url = ${tomlQuote(url)}
wire_api = "responses"
requires_openai_auth = true
env_key = "AI_AUTH_KEY"
stream_idle_timeout_ms = 10000000

[profiles.llama]
model = ${tomlQuote(model)}
model_provider = "llama_cpp"
web_search = "disabled"`;
  }
</script>

<section class="flex flex-col gap-4">
  <p class="text-xs leading-relaxed text-txtsecondary">
    Use these details to call this server from external tools, scripts, and editors. OpenAI-compatible clients use <code>/v1</code>;
    Claude uses the server origin as its Anthropic base URL.
  </p>

  <div class="grid gap-3 md:grid-cols-2">
    <CopyField label="Base URL" value={activeBaseURL} placeholder="—">
      {#snippet headerActions()}
        <div class="inline-flex overflow-hidden rounded-[2px] border border-border bg-black">
          <button
            type="button"
            class="api-toggle"
            class:api-toggle--active={baseUrlKind === "openai"}
            onclick={() => (baseUrlKind = "openai")}
            aria-pressed={baseUrlKind === "openai"}
          >
            OpenAI
          </button>
          <button
            type="button"
            class="api-toggle"
            class:api-toggle--active={baseUrlKind === "anthropic"}
            onclick={() => (baseUrlKind = "anthropic")}
            aria-pressed={baseUrlKind === "anthropic"}
          >
            Anthropic
          </button>
        </div>
      {/snippet}
    </CopyField>
    <CopyField
      label={authRequired ? "API Key · Required" : "API Key · Optional"}
      value={apiKey}
      secret={true}
      placeholder={authRequired ? "—" : "(no API key required)"}
    />
  </div>

  <div class="flex items-center gap-2 font-mono text-[10px] uppercase tracking-widest">
    <span class="text-txtmuted">Examples use model</span>
    <span class="truncate text-txtmain" title={snippetModel}>{snippetModel}</span>
    {#if snippetModelState === "ready"}
      <span class="rounded-[2px] border border-success/40 px-1.5 py-0.5 text-[9px] text-success">
        Active
      </span>
    {:else if snippetModelState === "loading"}
      <span class="rounded-[2px] border border-warning/40 px-1.5 py-0.5 text-[9px] text-warning">
        Loading
      </span>
    {:else}
      <span class="rounded-[2px] border border-border px-1.5 py-0.5 text-[9px] text-txtmuted">
        Default
      </span>
    {/if}
  </div>

  <div class="flex items-center gap-0 border-b border-border" role="tablist" aria-label="Connection guide">
    {#each tabs as tab (tab.id)}
      <button
        type="button"
        role="tab"
        aria-selected={activeTab === tab.id}
        class="guide-tab"
        class:guide-tab--active={activeTab === tab.id}
        onclick={() => (activeTab = tab.id)}
      >
        {tab.label}
      </button>
    {/each}
  </div>

  {#if activeTab === "curl"}
    <div class="grid gap-4">
      <SnippetBlock label="curl" text={curlSnippet} copyLabel="curl snippet" />
      <SnippetBlock label="curl streaming" text={streamingCurlSnippet} copyLabel="streaming curl snippet" />
    </div>
  {:else if activeTab === "python"}
    <SnippetBlock label="Python (openai)" text={pythonSnippet} copyLabel="Python snippet" />
  {:else if activeTab === "claude"}
    <div class="rounded-[2px] border border-border bg-zinc-950/60 p-3">
      <div class="mb-3 flex items-center justify-between gap-3">
        <h3 class="font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary">
          Claude
        </h3>
        <span class="font-mono text-[10px] uppercase tracking-widest text-txtmuted">
          Run with <span class="pill pill--accent">cllama</span>
        </span>
      </div>
      <ol class="mb-3 grid gap-1.5 text-xs leading-relaxed text-txtsecondary">
        <li class="flex items-start gap-2">
          <span class="step">1</span>
          <span>Append the snippet below to <span class="pill">~/.bashrc</span>.</span>
        </li>
        <li class="flex items-start gap-2">
          <span class="step">2</span>
          <span>Restart your shell.</span>
        </li>
        <li class="flex items-start gap-2">
          <span class="step">3</span>
          <span>Run <span class="pill pill--accent">cllama</span> to start Claude.</span>
        </li>
      </ol>
      <SnippetBlock label="~/.bashrc shortcut" text={claudeShortcutSnippet} copyLabel="Claude shortcut" />
    </div>
  {:else if activeTab === "codex"}
    <div class="rounded-[2px] border border-border bg-zinc-950/60 p-3">
      <div class="mb-3 flex items-center justify-between gap-3">
        <h3 class="font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary">
          Codex
        </h3>
        <span class="font-mono text-[10px] uppercase tracking-widest text-txtmuted">
          Run with <span class="pill pill--accent">cdllama</span>
        </span>
      </div>
      <ol class="mb-3 grid gap-1.5 text-xs leading-relaxed text-txtsecondary">
        <li class="flex items-start gap-2">
          <span class="step">1</span>
          <span>Save the profile to <span class="pill">~/.codex.toml</span>.</span>
        </li>
        <li class="flex items-start gap-2">
          <span class="step">2</span>
          <span>Append the shortcut to <span class="pill">~/.bashrc</span>.</span>
        </li>
        <li class="flex items-start gap-2">
          <span class="step">3</span>
          <span>Restart your shell.</span>
        </li>
        <li class="flex items-start gap-2">
          <span class="step">4</span>
          <span>Run <span class="pill pill--accent">cdllama</span> to start Codex.</span>
        </li>
      </ol>
      <div class="grid gap-4">
        <SnippetBlock label="~/.codex.toml" text={codexTomlSnippet} copyLabel="Codex TOML profile" />
        <SnippetBlock label="~/.bashrc shortcut" text={codexBashSnippet} copyLabel="Codex shortcut" />
      </div>
    </div>
  {/if}
</section>

<style>
  .pill {
    display: inline-flex;
    align-items: center;
    padding: 1px 6px;
    border: 1px solid var(--color-border);
    border-radius: 2px;
    background: #000000;
    color: var(--color-txtmain);
    font-family: var(--font-mono);
    font-size: 11px;
    line-height: 1.4;
    letter-spacing: normal;
    text-transform: none;
    white-space: nowrap;
  }

  .pill--accent {
    color: #ffffff;
    border-color: color-mix(in srgb, var(--color-success) 60%, var(--color-border));
    background: color-mix(in srgb, var(--color-success) 10%, #000000);
  }

  .api-toggle {
    padding: 2px 8px;
    background: transparent;
    color: var(--color-txtsecondary);
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    line-height: 1.4;
    transition: color 150ms ease, background 150ms ease;
    cursor: pointer;
  }

  .api-toggle:not(:first-child) {
    border-left: 1px solid var(--color-border);
  }

  .api-toggle:hover {
    color: var(--color-txtmain);
  }

  .api-toggle--active {
    background: color-mix(in srgb, var(--color-success) 12%, #000000);
    color: #ffffff;
  }

  .guide-tab {
    padding: 0.5rem 0.875rem;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--color-txtsecondary);
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    cursor: pointer;
    transition: color 150ms ease, border-color 150ms ease;
  }

  .guide-tab:hover {
    color: var(--color-txtmain);
  }

  .guide-tab--active {
    color: #ffffff;
    border-bottom-color: #ffffff;
  }

  .step {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    margin-top: 1px;
    border: 1px solid var(--color-border);
    border-radius: 9999px;
    background: #000000;
    color: var(--color-txtsecondary);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 700;
    line-height: 1;
  }
</style>
