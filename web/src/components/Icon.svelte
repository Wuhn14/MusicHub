<script lang="ts" module>
  // 24×24 icon paths used by the player and the track lists.
  const paths = {
    play: 'M8 5.14v13.72a1 1 0 0 0 1.52.85l10.6-6.86a1 1 0 0 0 0-1.7L9.52 4.29A1 1 0 0 0 8 5.14Z',
    pause: 'M7 4h3.5v16H7V4Zm6.5 0H17v16h-3.5V4Z',
    prev: 'M6 5h2v14H6V5Zm3.5 7 9.5 6.5v-13L9.5 12Z',
    next: 'M16 5h2v14h-2V5ZM5 18.5 14.5 12 5 5.5v13Z',
    shuffle:
      'M10.59 9.17 5.41 4 4 5.41l5.17 5.17 1.42-1.41ZM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5Zm.33 9.41-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13Z',
    repeat:
      'M7 7h10v3l4-4-4-4v3H5v6h2V7Zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4Z',
    queue: 'M3 6h13v2H3V6Zm0 4h13v2H3v-2Zm0 4h9v2H3v-2Zm13 0 6 3.5-6 3.5v-7Z',
    volume: 'M3 9v6h4l5 5V4L7 9H3Zm13.5 3a4.5 4.5 0 0 0-2.5-4.03v8.05A4.5 4.5 0 0 0 16.5 12ZM14 3.23v2.06a7 7 0 0 1 0 13.42v2.06a9 9 0 0 0 0-17.54Z',
    volumeLow: 'M3 9v6h4l5 5V4L7 9H3Zm13.5 3a4.5 4.5 0 0 0-2.5-4.03v8.05A4.5 4.5 0 0 0 16.5 12Z',
    mute: 'M3 9v6h4l5 5V4L7 9H3Zm13.59 3L14 9.41 15.41 8 18 10.59 20.59 8 22 9.41 19.41 12 22 14.59 20.59 16 18 13.41 15.41 16 14 14.59 16.59 12Z',
    close: 'M6.4 5 12 10.6 17.6 5 19 6.4 13.4 12l5.6 5.6-1.4 1.4-5.6-5.6L6.4 19 5 17.6l5.6-5.6L5 6.4 6.4 5Z',
    chevronDown: 'M5.6 8.3 12 14.7l6.4-6.4 1.4 1.4-7.8 7.8-7.8-7.8 1.4-1.4Z',
    up: 'M12 6.6 19.4 14 18 15.4l-6-6-6 6L4.6 14 12 6.6Z',
    down: 'M12 17.4 4.6 10 6 8.6l6 6 6-6 1.4 1.4-7.4 7.4Z',
    drag: 'M9 5a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Zm6 0a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Zm-6 5.5a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Zm6 0a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3ZM9 16a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Zm6 0a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Z',
    more: 'M5 10.5a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Zm7 0a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Zm7 0a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Z',
    note: 'M12 3v10.55A4 4 0 1 0 14 17V7h4V3h-6Z',
  } as const;

  export type IconName = keyof typeof paths;
</script>

<script lang="ts">
  interface Props {
    name: IconName;
    size?: number;
  }

  let { name, size = 20 }: Props = $props();
</script>

<svg viewBox="0 0 24 24" width={size} height={size} aria-hidden="true" focusable="false">
  <path fill="currentColor" d={paths[name]} />
</svg>

<style>
  svg {
    display: block;
    flex: none;
  }
</style>
