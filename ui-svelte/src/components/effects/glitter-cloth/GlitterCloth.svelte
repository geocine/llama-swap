<script lang="ts">
  import type { ComponentProps } from "svelte";
  import Scene from "./GlitterClothScene.svelte";

  type SceneProps = ComponentProps<typeof Scene>;

  interface Props {
    class?: string;
    color?: SceneProps["color"];
    speed?: SceneProps["speed"];
    brightness?: SceneProps["brightness"];
    blendStrength?: SceneProps["blendStrength"];
    noiseScale?: SceneProps["noiseScale"];
    vignetteStrength?: SceneProps["vignetteStrength"];
    vignettePower?: SceneProps["vignettePower"];
    vignetteOpacity?: SceneProps["vignetteOpacity"];
  }

  let {
    class: className = "",
    color = "#FF6900",
    speed = 1.0,
    brightness = 1.0,
    blendStrength = 0.02,
    noiseScale = 4.0,
    vignetteStrength = 15.0,
    vignettePower = 0.25,
    vignetteOpacity = 1.0,
  }: Props = $props();
</script>

<div class="relative h-full w-full overflow-hidden {className}">
  <div class="absolute inset-0 z-0">
    <Scene
      {color}
      {speed}
      {brightness}
      {blendStrength}
      {noiseScale}
      {vignetteStrength}
      {vignettePower}
      {vignetteOpacity}
    />
  </div>
</div>
