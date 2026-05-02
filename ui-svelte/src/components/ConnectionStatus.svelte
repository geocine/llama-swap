<script lang="ts">
  import { connectionState } from "../stores/theme";

  let eventStatusColor = $derived.by(() => {
    switch ($connectionState) {
      case "connected":
        return "bg-emerald-500";
      case "connecting":
        return "bg-amber-500";
      case "disconnected":
      default:
        return "bg-red-500";
    }
  });
</script>

<div class="flex items-center ml-2" aria-label="Event stream status: {$connectionState ?? 'unknown'}">
  <span class="inline-block w-2.5 h-2.5 rounded-full {eventStatusColor}"></span>
</div>
