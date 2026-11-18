<script lang="ts">
  // Loading and error states shared by the pages.
  interface Props {
    loading?: boolean;
    error?: string;
    loadingText?: string;
  }

  let { loading = false, error = '', loadingText = 'Chargement…' }: Props = $props();
</script>

{#if error}
  <div class="status error" role="alert">
    <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
      <path
        fill="currentColor"
        d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Zm1 15h-2v-2h2v2Zm0-4h-2V7h2v6Z"
      />
    </svg>
    <span>{error}</span>
  </div>
{:else if loading}
  <div class="status" role="status">
    <span class="spinner" aria-hidden="true"></span>
    <span>{loadingText}</span>
  </div>
{/if}

<style>
  .status {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 0;
    color: var(--text-muted);
  }

  .error {
    padding: 14px 16px;
    border-radius: var(--radius);
    background: rgb(255 107 107 / 0.1);
    color: var(--danger);
  }

  .spinner {
    width: 18px;
    height: 18px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
