<script lang="ts">
  export let onGoToolbox: () => void;
  export let title: string = 'No drive selected';
  export let hint: string = 'Plug in your USB stick (or pick a staging folder) in the Toolbox, then come back — your games stay put.';
</script>

<div class="nodrive" role="status">
  <svg viewBox="0 0 24 24" width="40" height="40" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">
    <rect x="3" y="13" width="18" height="7" rx="2" />
    <path d="M7 13V9a5 5 0 0 1 10 0v4" stroke-linecap="round" />
    <circle cx="17.5" cy="16.5" r="0.6" fill="currentColor" />
  </svg>
  <div>
    <strong>{title}</strong>
    <p class="muted">{hint}</p>
  </div>
  <button class="btn-primary" onclick={onGoToolbox}>Open Toolbox</button>
</div>

<style>
  .nodrive {
    display: flex;
    gap: 18px;
    align-items: center;
    padding: 28px 30px;
    border: 1.5px dashed var(--border);
    border-radius: 16px;
    background: var(--surface);
    color: var(--fg-muted);
    margin-bottom: 20px;
    flex-wrap: wrap;
  }
  .nodrive strong {
    color: var(--fg);
    font-size: 16px;
    display: block;
    margin-bottom: 4px;
  }
  .nodrive p {
    margin: 0;
    max-width: 60ch;
  }
  .nodrive button {
    margin-left: auto;
  }
</style>
