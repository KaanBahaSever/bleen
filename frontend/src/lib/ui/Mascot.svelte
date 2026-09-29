<script lang="ts">
  // The bleen bean (docs/architecture.md §6). Only the eyes change with mood.
  type Mood = 'idle' | 'working' | 'happy' | 'concerned' | 'sleeping';
  let { mood = 'idle', size = 96 }: { mood?: Mood; size?: number } = $props();
  const uid = Math.random().toString(36).slice(2, 8);
  const bean =
    'M33 37C41.3 33.2 53.7 41.5 64 41.5C74.3 41.5 86.7 33.2 95 37C103.3 40.8 114 55 114 64C114 73 103.3 85.8 95 91C86.7 96.2 74.3 95 64 95C53.7 95 41.3 96.2 33 91C24.7 85.8 14 73 14 64C14 55 24.7 40.8 33 37Z';
</script>

<svg
  class="mascot {mood}"
  width={size}
  height={size}
  viewBox="0 0 128 128"
  aria-hidden="true"
>
  <defs>
    <clipPath id="bean-{uid}"><path d={bean} /></clipPath>
  </defs>
  <g class="body" style="transform-origin: 64px 64px">
    <g transform="rotate({mood === 'concerned' ? -16 : -10} 64 64)">
      <path d={bean} fill="#6FD3A5" />
      <path clip-path="url(#bean-{uid})" d="M0 0H66C58 50 72 74 62 128H0Z" fill="#7FB0F4" />
      <path d="M25 57C27 50 32 46 39 45" fill="none" stroke="#fff" stroke-opacity=".6" stroke-width="4.5" stroke-linecap="round" />
      {#if mood === 'happy'}
        <path d="M49 65q4-5 8 0M71 65q4-5 8 0" stroke="#1F2328" stroke-width="2.5" fill="none" stroke-linecap="round" />
      {:else if mood === 'sleeping'}
        <path d="M49 64q4 3 8 0M71 64q4 3 8 0" stroke="#1F2328" stroke-width="2.5" fill="none" stroke-linecap="round" />
      {:else if mood === 'concerned'}
        <g class="eyes"><ellipse cx="53" cy="63" rx="2.6" ry="3.4" fill="#1F2328" /><ellipse cx="75" cy="63" rx="2.6" ry="3.4" fill="#1F2328" /></g>
      {:else}
        <g class="eyes" class:look={mood === 'working'}>
          <ellipse cx="53" cy="63" rx="3.6" ry="5" fill="#1F2328" />
          <ellipse cx="75" cy="63" rx="3.6" ry="5" fill="#1F2328" />
        </g>
      {/if}
    </g>
  </g>
</svg>

<style>
  .mascot {
    overflow: visible;
    flex: none;
  }
  .idle .body,
  .sleeping .body {
    animation: breathe 4s ease-in-out infinite;
  }
  .happy .body {
    animation: hop 520ms var(--ease-spring) 1;
  }
  .eyes {
    transform-origin: 64px 63px;
    animation: blink 6.5s infinite;
  }
  .eyes.look {
    animation: look 2.4s ease-in-out infinite;
  }
  @keyframes breathe {
    50% {
      transform: scale(1.025);
    }
  }
  @keyframes hop {
    40% {
      transform: translateY(-10px);
    }
  }
  @keyframes blink {
    0%, 96%, 100% {
      transform: scaleY(1);
    }
    98% {
      transform: scaleY(0.1);
    }
  }
  @keyframes look {
    0%, 100% {
      transform: translateX(-2px);
    }
    50% {
      transform: translateX(2.5px);
    }
  }
</style>
