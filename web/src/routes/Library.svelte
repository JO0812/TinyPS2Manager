<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Destination, type LibraryItem } from '../lib/api';
  import GameCard from '../components/GameCard.svelte';
  import DestinationPanel from '../components/DestinationPanel.svelte';

  export let onGoDrive: () => void;

  let items: LibraryItem[] = [];
  let destinations: Destination[] = [];
  let selectedId = 0;
  let query = '';
  let platform: 'all' | 'ps2' | 'ps1' = 'all';
  let sort: 'name' | 'largest' | 'smallest' = 'name';
  let viewMode: 'grid' | 'list' = 'grid';
  let importPath = '';
  let notice = '';
  let noticeKind: 'ok' | 'err' = 'ok';
  let loading = true;
  let searchEl: HTMLInputElement;

  async function refresh() {
    try {
      const [libs, dests] = await Promise.all([api.library(), api.destinations()]);
      items = libs;
      destinations = dests;
      if (!dests.some((d) => d.id === selectedId)) {
        selectedId = dests[0]?.id ?? 0;
        localStorage.setItem('oplbm.destId', String(selectedId));
      }
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    } finally {
      loading = false;
    }
  }

  function pickDestination(id: number) {
    selectedId = id;
    localStorage.setItem('oplbm.destId', String(id));
  }

  async function doImport() {
    if (!importPath.trim()) return;
    notice = '';
    try {
      const fresh = await api.importDir(importPath.trim());
      importPath = '';
      notice = `Imported ${fresh.length} titles.`;
      noticeKind = 'ok';
      await refresh();
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  $: filtered = items
    .filter((it) => (platform === 'all' ? true : it.platform === platform))
    .filter((it) => (query.trim() ? it.title.toLowerCase().includes(query.trim().toLowerCase()) : true))
    .sort((a, b) => {
      if (sort === 'largest') return b.sizeBytes - a.sizeBytes;
      if (sort === 'smallest') return a.sizeBytes - b.sizeBytes;
      return a.title.localeCompare(b.title);
    });
  $: fsBadge = (() => {
    const d = destinations.find((x) => x.id === selectedId);
    return d ? (d.fsOverride || d.filesystem).toUpperCase() : '';
  })();

  function hotkeys(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      searchEl?.focus();
    }
    if (e.key === 'Escape') searchEl?.blur();
  }

  onMount(() => {
    selectedId = Number(localStorage.getItem('oplbm.destId') || '0');
    refresh();
    window.addEventListener('keydown', hotkeys);
    return () => window.removeEventListener('keydown', hotkeys);
  });
</script>

<p class="eyebrow">Game Vault</p>
<div class="title-row">
  <h1 class="view-title">Your game library</h1>
  <div class="title-actions">
    {#if fsBadge}
      <span class="pill pill-green">{fsBadge}</span>
    {/if}
    <button class="btn-primary" onclick={onGoDrive}>Prepare external drive</button>
  </div>
</div>

<div class="toolbar">
  <div class="search">
    <span aria-hidden="true">⌕</span>
    <input
      bind:this={searchEl}
      bind:value={query}
      placeholder="Search {items.length} installed games"
      aria-label="Search games"
    />
    <kbd>⌘K</kbd>
  </div>
  <select bind:value={platform} aria-label="Platform filter">
    <option value="all">All platforms</option>
    <option value="ps2">PS2</option>
    <option value="ps1">PS1</option>
  </select>
  <select bind:value={sort} aria-label="Sort order">
    <option value="name">Name A–Z</option>
    <option value="largest">Largest first</option>
    <option value="smallest">Smallest first</option>
  </select>
  <div class="view-toggle" role="group" aria-label="View mode">
    <button class:active={viewMode === 'grid'} onclick={() => (viewMode = 'grid')} title="Grid">▦</button>
    <button class:active={viewMode === 'list'} onclick={() => (viewMode = 'list')} title="List">☰</button>
  </div>
</div>

<DestinationPanel {destinations} {selectedId} gameCount={items.length} onSelect={pickDestination} />

{#if notice}
  <p class="notice" class:err={noticeKind === 'err'}>{notice}</p>
{/if}

<div class="import-row card">
  <input
    class="field"
    bind:value={importPath}
    placeholder="Source folder with .iso / .cue+.bin files…"
    aria-label="Source folder"
    onkeydown={(e) => {
      if (e.key === 'Enter') doImport();
    }}
  />
  <button class="btn-ghost" onclick={doImport}>Scan folder</button>
</div>

<h2 class="section-title">Installed games <span class="muted">{filtered.length}</span></h2>

{#if loading}
  <p class="muted">Loading…</p>
{:else if filtered.length === 0}
  <div class="card empty">
    {#if items.length === 0}
      <p>No games yet. Scan a source folder above to build your library.</p>
    {:else}
      <p>No games match this filter.</p>
    {/if}
  </div>
{:else if viewMode === 'grid'}
  <div class="grid">
    {#each filtered as item (item.id)}
      <GameCard {item} onChanged={refresh} />
    {/each}
  </div>
{:else}
  <div class="list">
    {#each filtered as item (item.id)}
      <GameCard {item} list onChanged={refresh} />
    {/each}
  </div>
{/if}

<style>
  .title-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 20px;
    flex-wrap: wrap;
  }
  .title-actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .toolbar {
    display: flex;
    gap: 10px;
    margin-bottom: 16px;
    flex-wrap: wrap;
  }
  .search {
    flex: 1;
    min-width: 220px;
    display: flex;
    align-items: center;
    gap: 8px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 0 12px;
  }
  .search input {
    flex: 1;
    background: none;
    border: none;
    padding: 10px 0;
    color: var(--fg);
  }
  .search input:focus {
    outline: none;
  }
  .search:focus-within {
    border-color: var(--accent-border);
  }
  kbd {
    font-size: 11px;
    color: var(--fg-faint);
    background: var(--surface-hover);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 2px 6px;
  }
  .toolbar select {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 10px 12px;
    color: var(--fg-muted);
  }
  .view-toggle {
    display: flex;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    overflow: hidden;
  }
  .view-toggle button {
    padding: 10px 14px;
    color: var(--fg-faint);
    font-size: 16px;
  }
  .view-toggle button.active {
    background: var(--surface-hover);
    color: var(--fg);
  }
  .notice {
    padding: 10px 14px;
    border-radius: 10px;
    background: var(--success-soft);
    color: var(--success);
    margin: 0 0 12px;
  }
  .notice.err {
    background: var(--danger-soft);
    color: var(--danger);
  }
  .import-row {
    display: flex;
    gap: 10px;
    padding: 12px;
    margin-bottom: 20px;
  }
  .section-title {
    font-size: 18px;
    margin: 0 0 14px;
  }
  .empty {
    padding: 28px;
    text-align: center;
    color: var(--fg-muted);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: 16px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
</style>
