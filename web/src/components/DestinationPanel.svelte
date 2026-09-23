<script lang="ts">
  import { formatBytes, type Destination } from '../lib/api';

  export let destinations: Destination[];
  export let selectedId: number;
  export let gameCount: number;
  export let onSelect: (id: number) => void;

  $: dest = destinations.find((d) => d.id === selectedId);
  $: fsLabel = dest ? (dest.fsOverride || dest.filesystem).toUpperCase() : '';
  $: hasCapacity = !!dest && dest.totalBytes > 0;
  $: used = dest && hasCapacity ? dest.totalBytes - dest.freeBytes : 0;
  $: usedPct = dest && hasCapacity ? Math.min(100, Math.max(0, (used / dest.totalBytes) * 100)) : 0;
</script>

<div class="card dest-panel">
  <div class="dest-id">
    <div class="dest-icon">▤</div>
    <div>
      <div class="muted small">Backup destination</div>
      {#if dest}
        <div class="dest-name">{dest.path.split('/').filter(Boolean).pop() ?? dest.path} · {dest.kind}</div>
        <div class="muted small">{dest.path}</div>
      {:else}
        <div class="dest-name">No destination yet</div>
        <div class="muted small">Add one under Drive Options</div>
      {/if}
    </div>
    {#if destinations.length > 1}
      <select
        class="field dest-select"
        value={selectedId}
        on:change={(e) => onSelect(Number(e.currentTarget.value))}
        aria-label="Destination"
      >
        {#each destinations as d}
          <option value={d.id}>{d.path}</option>
        {/each}
      </select>
    {/if}
  </div>

  <div class="dest-space">
    {#if dest}
      <div class="space-row">
        <strong>{hasCapacity ? `${formatBytes(used)} used` : `${formatBytes(dest.freeBytes)} free`}</strong>
        <span class="muted">
          {#if hasCapacity}
            {formatBytes(dest.freeBytes)} free of {formatBytes(dest.totalBytes)}
          {:else}
            capacity unknown — set a filesystem to probe it
          {/if}
        </span>
      </div>
      {#if hasCapacity}
        <div class="progress-track"><div class="progress-fill" style="width: {usedPct}%"></div></div>
      {/if}
      <div class="muted small">{gameCount} titles in library</div>
    {:else}
      <div class="muted">Nothing to show yet.</div>
    {/if}
  </div>

  <div class="dest-meta">
    {#if dest}
      <span class="pill pill-green">{fsLabel || 'UNKNOWN'}</span>
    {/if}
  </div>
</div>

<style>
  .dest-panel {
    display: flex;
    gap: 28px;
    padding: 20px 24px;
    align-items: center;
    flex-wrap: wrap;
  }
  .dest-id {
    display: flex;
    gap: 14px;
    align-items: center;
    min-width: 240px;
  }
  .dest-icon {
    width: 44px;
    height: 44px;
    border-radius: 12px;
    background: var(--accent-soft);
    color: var(--accent);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 22px;
    flex-shrink: 0;
  }
  .dest-name {
    font-weight: 700;
    font-size: 16px;
  }
  .small {
    font-size: 12px;
  }
  .dest-select {
    width: auto;
    margin-left: 8px;
  }
  .dest-space {
    flex: 1;
    min-width: 240px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .space-row {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .dest-meta {
    display: flex;
    flex-direction: column;
    gap: 8px;
    align-items: flex-end;
  }
</style>
