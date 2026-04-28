<script lang="ts">
  interface HistogramData {
    bins: number[];
    min: number;
    max: number;
    binSize: number;
    p99: number;
    p95: number;
    p50: number;
  }

  interface Props {
    data: HistogramData;
  }

  let { data }: Props = $props();

  const height = 120;
  const padding = { top: 10, right: 15, bottom: 25, left: 45 };
  const viewBoxWidth = 600;
  const chartWidth = viewBoxWidth - padding.left - padding.right;
  const chartHeight = height - padding.top - padding.bottom;

  let maxCount = $derived(Math.max(...data.bins));
  let barWidth = $derived(chartWidth / data.bins.length);
  let range = $derived(data.max - data.min);

  function getXPosition(value: number): number {
    return padding.left + ((value - data.min) / range) * chartWidth;
  }
</script>

<div class="mt-2 w-full">
  <svg viewBox="0 0 {viewBoxWidth} {height}" class="w-full h-auto" preserveAspectRatio="xMidYMid meet">
    <!-- Y-axis -->
    <line
      x1={padding.left}
      y1={padding.top}
      x2={padding.left}
      y2={height - padding.bottom}
      stroke="#3f3f46"
      stroke-width="1"
    />

    <!-- X-axis -->
    <line
      x1={padding.left}
      y1={height - padding.bottom}
      x2={viewBoxWidth - padding.right}
      y2={height - padding.bottom}
      stroke="#3f3f46"
      stroke-width="1"
    />

    <!-- Histogram bars -->
    {#each data.bins as count, i}
      {@const barHeight = maxCount > 0 ? (count / maxCount) * chartHeight : 0}
      {@const x = padding.left + i * barWidth}
      {@const y = height - padding.bottom - barHeight}
      {@const binStart = data.min + i * data.binSize}
      {@const binEnd = binStart + data.binSize}
      <g>
        <rect
          {x}
          {y}
          width={Math.max(barWidth - 1, 1)}
          height={barHeight}
          fill="#71717a"
          opacity="0.6"
          class="hover:opacity-90 transition-opacity cursor-pointer"
        />
        <title>{`${binStart.toFixed(1)} - ${binEnd.toFixed(1)} tokens/sec\nCount: ${count}`}</title>
      </g>
    {/each}

    <!-- Percentile lines -->
    <line
      x1={getXPosition(data.p50)}
      y1={padding.top}
      x2={getXPosition(data.p50)}
      y2={height - padding.bottom}
      stroke="#a1a1aa"
      stroke-width="2"
      stroke-dasharray="4 2"
    />

    <line
      x1={getXPosition(data.p95)}
      y1={padding.top}
      x2={getXPosition(data.p95)}
      y2={height - padding.bottom}
      stroke="#f59e0b"
      stroke-width="2"
      stroke-dasharray="4 2"
    />

    <line
      x1={getXPosition(data.p99)}
      y1={padding.top}
      x2={getXPosition(data.p99)}
      y2={height - padding.bottom}
      stroke="#22c55e"
      stroke-width="2"
      stroke-dasharray="4 2"
    />

    <!-- X-axis labels -->
    <text x={padding.left} y={height - 5} font-size="10" fill="#71717a" text-anchor="start" font-family="monospace">
      {data.min.toFixed(1)}
    </text>

    <text x={viewBoxWidth - padding.right} y={height - 5} font-size="10" fill="#71717a" text-anchor="end" font-family="monospace">
      {data.max.toFixed(1)}
    </text>

    <!-- X-axis label -->
    <text x={padding.left + chartWidth / 2} y={height - 2} font-size="9" fill="#52525b" text-anchor="middle" font-family="monospace" letter-spacing="0.05em">
      TOKENS/SECOND DISTRIBUTION
    </text>
  </svg>
</div>
