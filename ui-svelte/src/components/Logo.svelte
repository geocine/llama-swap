<script lang="ts">
  interface Props {
    size?: number;
    class?: string;
    title?: string;
  }
  let { size = 20, class: className = "", title = "llama-swap" }: Props = $props();
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  viewBox="0 0 32 32"
  width={size}
  height={size}
  role="img"
  aria-label={title}
  class={className}
>
  <title>{title}</title>
  <!-- Wordmark "ls" — kept as a text node so it scales cleanly inside header
       chrome and inherits the parent's `currentColor`. -->
  <text
    x="16"
    y="22"
    text-anchor="middle"
    fill="currentColor"
    font-family="ui-monospace, 'JetBrains Mono', 'Fira Mono', 'Consolas', monospace"
    font-size="20"
    font-weight="800"
    letter-spacing="-1"
  >ls</text>
</svg>
