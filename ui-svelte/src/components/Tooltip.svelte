<script lang="ts">
  interface Props {
    content: string;
  }

  let { content }: Props = $props();
</script>

<div class="relative group inline-block">
  <span class="cursor-help text-txtsecondary">&#9432;</span>
  <div
    class="absolute top-full left-1/2 transform -translate-x-1/2 mt-2
           px-3 py-2 bg-surface-elevated text-txtmain text-xs rounded-sm
           opacity-0 group-hover:opacity-100 transition-opacity
           duration-150 pointer-events-none whitespace-nowrap z-50 normal-case
           border border-border shadow-2xl"
  >
    {content}
    <div class="absolute bottom-full left-1/2 transform -translate-x-1/2 border-4 border-transparent border-b-surface-elevated"></div>
  </div>
</div>
