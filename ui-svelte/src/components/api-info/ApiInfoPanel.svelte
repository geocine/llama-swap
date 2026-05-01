<script lang="ts">
  import { models, serverInfo } from "../../stores/api";
  import { Check, Copy } from "lucide-svelte";
  import CopyField from "./CopyField.svelte";

  let baseURL = $state("");
  $effect(() => {
    if (typeof window !== "undefined") {
      baseURL = `${window.location.origin}/v1`;
    }
  });

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

  let apiKey = $derived($serverInfo?.apiKey ?? "");
  let authRequired = $derived($serverInfo?.authRequired ?? false);

  let curlSnippet = $derived(buildCurlSnippet(baseURL, apiKey, snippetModel));
  let pythonSnippet = $derived(buildPythonSnippet(baseURL, apiKey, snippetModel));

  function buildCurlSnippet(url: string, key: string, model: string): string {
    const auth = key ? `\n  -H "Authorization: Bearer ${key}" \\` : "";
    return `curl ${url}/chat/completions \\${auth}
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${model}",
    "messages": [{"role": "user", "content": "Hello"}]
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

  let copiedSnippet = $state<"curl" | "python" | null>(null);
  let copyTimer: ReturnType<typeof setTimeout> | null = null;

  async function copySnippet(name: "curl" | "python", text: string) {
    try {
      await navigator.clipboard.writeText(text);
      copiedSnippet = name;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copiedSnippet = null), 1500);
    } catch (err) {
      console.error("Failed to copy", err);
    }
  }
</script>

<section class="flex flex-col gap-4">
  <p class="text-xs leading-relaxed text-txtsecondary">
    Use these details to call this server from external tools, scripts, and editors. The OpenAI-compatible API lives at the base URL below.
  </p>

  <div class="grid gap-3 md:grid-cols-2">
    <CopyField label="Base URL" value={baseURL} placeholder="—" />
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

  <div>
    <div class="mb-1.5 flex items-center justify-between gap-2">
      <span class="font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary">
        curl
      </span>
      <button
        class="flex items-center gap-1.5 rounded-[2px] border border-border bg-zinc-950 px-2 py-1 font-mono text-[10px] font-bold uppercase tracking-wider text-txtsecondary transition-colors duration-150 hover:border-border-hover hover:text-white"
        onclick={() => copySnippet("curl", curlSnippet)}
        title="Copy snippet"
        aria-label="Copy curl snippet"
      >
        {#if copiedSnippet === "curl"}
          <Check class="h-3 w-3 text-success" />
          Copied
        {:else}
          <Copy class="h-3 w-3" />
          Copy
        {/if}
      </button>
    </div>
    <pre class="overflow-x-auto rounded-[2px] border border-border bg-black px-3 py-2 font-mono text-[11px] leading-relaxed text-txtmain">{curlSnippet}</pre>
  </div>

  <div>
    <div class="mb-1.5 flex items-center justify-between gap-2">
      <span class="font-mono text-[10px] font-bold uppercase tracking-widest text-txtsecondary">
        Python (openai)
      </span>
      <button
        class="flex items-center gap-1.5 rounded-[2px] border border-border bg-zinc-950 px-2 py-1 font-mono text-[10px] font-bold uppercase tracking-wider text-txtsecondary transition-colors duration-150 hover:border-border-hover hover:text-white"
        onclick={() => copySnippet("python", pythonSnippet)}
        title="Copy snippet"
        aria-label="Copy Python snippet"
      >
        {#if copiedSnippet === "python"}
          <Check class="h-3 w-3 text-success" />
          Copied
        {:else}
          <Copy class="h-3 w-3" />
          Copy
        {/if}
      </button>
    </div>
    <pre class="overflow-x-auto rounded-[2px] border border-border bg-black px-3 py-2 font-mono text-[11px] leading-relaxed text-txtmain">{pythonSnippet}</pre>
  </div>
</section>
