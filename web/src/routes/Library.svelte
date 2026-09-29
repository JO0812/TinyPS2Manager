<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type LibraryItem } from '../lib/api';
  import { activeDest, refreshDrives } from '../lib/drive';
  import GameCard from '../components/GameCard.svelte';
  import NoDrive from '../components/NoDrive.svelte';
  import { browseFolder } from '../lib/dialog';

  export let onGoToolbox: () => void;

  let items: LibraryItem[] = [];
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
      const [libs] = await Promise.all([api.library(), refreshDrives()]);
      items = libs;
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    } finally {
      loading = false;
    }
  }

  async function doImport() {
    if (!importPath.trim()) {
      notice = 'Type a source folder path first.';
      noticeKind = 'err';
      return;
    }
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
  /** Group rows by multi-disc set (singles stand alone), ordered by title. */
  $: rows = (() => {
    const byGroup = new Map<string, LibraryItem[]>();
    for (const it of filtered) {
      const key = it.discGroupId != null ? `group-${it.discGroupId}` : `single-${it.id}`;
      const list = byGroup.get(key) ?? [];
      list.push(it);
      byGroup.set(key, list);
    }
    return [...byGroup.values()]
      .map((members) => {
        const rep = [...members].sort((a, b) => a.discIndex - b.discIndex || a.id - b.id)[0];
        const bytes = members.reduce((n, m) => n + m.sizeBytes, 0);
        return { rep, ids: members.map((m) => m.id), count: members.length, bytes };
      })
      .sort((a, b) => {
        if (sort === 'largest') return b.bytes - a.bytes;
        if (sort === 'smallest') return a.bytes - b.bytes;
        return a.rep.title.localeCompare(b.rep.title);
      });
  })();

  async function browse() {
    const path = await browseFolder('Choose source folder');
    if (path) {
      importPath = path;
      notice = '';
    } else {
      notice = 'Folder picker is only available in the desktop app — type the path.';
      noticeKind = 'err';
    }
  }

  function hotkeys(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      searchEl?.focus();
    }
    if (e.key === 'Escape') searchEl?.blur();
  }

  onMount(() => {
    refresh();
    window.addEventListener('keydown', hotkeys);
    return () => window.removeEventListener('keydown', hotkeys);
  });
</script>

<div class="title-row">
  <h1 class="view-title">Games <span class="muted count">{rows.length}</span></h1>
</div>

{#if !$activeDest && !loading}
  <NoDrive onGoToolbox={onGoToolbox} />
{/if}

<div class="toolbar">
  <div class="search">
    <span aria-hidden="true">⌕</span>
    <input
      bind:this={searchEl}
      bind:value={query}
      placeholder="Search {items.length} games"
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
    <button class:active={viewMode === 'grid'} aria-pressed={viewMode === 'grid'} onclick={() => (viewMode = 'grid')} title="Grid">▦</button>
    <button class:active={viewMode === 'list'} aria-pressed={viewMode === 'list'} onclick={() => (viewMode = 'list')} title="List">☰</button>
  </div>
</div>

{#if notice}
  <p class="notice" class:err={noticeKind === 'err'} role={noticeKind === 'err' ? 'alert' : 'status'}>{notice}</p>
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
  <button class="btn-ghost" onclick={browse}>Browse…</button>
  <button class="btn-ghost" disabled={!importPath.trim()} onclick={doImport}>Scan folder</button>
</div>

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
    {#each rows as row (row.rep.id)}
      <GameCard item={row.rep} groupIds={row.ids} discCount={row.count} onChanged={refresh} />
    {/each}
  </div>
{:else}
  <div class="list">
    {#each rows as row (row.rep.id)}
      <GameCard item={row.rep} groupIds={row.ids} discCount={row.count} list onChanged={refresh} />
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
  .view-title {
    margin: 0;
  }
  .count {
    font-size: 18px;
    font-weight: 400;
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
    color: var(--fg-muted);
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
    color: var(--fg-muted);
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
