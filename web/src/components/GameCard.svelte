<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type LibraryItem } from '../lib/api';

  export let item: LibraryItem;
  export let list: boolean = false;
  export let onChanged: () => void;
  // Group enqueue: for multi-disc rows this carries every member id so one
  // click queues the whole set (manifests reference all discs). Per-item
  // actions (rename, disc type) act on the representative row item.
  export let groupIds: number[] = [];
  export let discCount: number = 1;

  $: enqueueIds = groupIds.length > 0 ? groupIds : [item.id];

  let menuOpen = false;
  let renaming = false;
  let draftTitle = '';
  let busy = '';
  let error = '';

  /** Placeholder tile: monogram over a PS2-blue gradient (art lands in M5). */
  function monogram(title: string): string {
    const words = title.replace(/\(.*?\)/g, '').trim().split(/\s+/);
    const letters = words.slice(0, 2).map((w) => w[0] ?? '').join('');
    return (letters || '?').toUpperCase();
  }

  function hue(title: string): number {
    let h = 0;
    for (const c of title) h = (h * 31 + c.charCodeAt(0)) % 360;
    // Keep tiles in the blue family: 200–230deg.
    return 200 + (h % 30);
  }

  async function enqueue() {
    menuOpen = false;
    const destId = Number(localStorage.getItem('oplbm.destId') || '0');
    if (!destId) {
      error = 'Pick a destination first (header panel).';
      return;
    }
    busy = 'queue';
    error = '';
    try {
      await api.enqueue(destId, enqueueIds);
      onChanged();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = '';
    }
  }

  async function enqueueEmber() {
    menuOpen = false;
    const destId = Number(localStorage.getItem('oplbm.destId') || '0');
    if (!destId) {
      error = 'Pick a destination first.';
      return;
    }
    busy = 'queue';
    error = '';
    try {
      await api.enqueue(destId, enqueueIds, 'copy-ps1-ember');
      onChanged();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = '';
    }
  }

  async function setType(discType: string) {
    menuOpen = false;
    busy = 'type';
    error = '';
    try {
      await api.patchLibrary(item.id, { discType });
      onChanged();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = '';
    }
  }

  function startRename() {
    draftTitle = item.title;
    renaming = true;
    menuOpen = false;
  }

  async function saveRename() {
    if (!draftTitle.trim() || draftTitle === item.title) {
      renaming = false;
      return;
    }
    busy = 'rename';
    error = '';
    try {
      await api.patchLibrary(item.id, { title: draftTitle.trim() });
      renaming = false;
      onChanged();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = '';
    }
  }

  function statusClass(s: string): string {
    if (s === 'done') return 'pill-green';
    if (s === 'error') return 'pill-danger';
    if (s === 'queued') return 'pill-purple';
    return 'pill-gray';
  }

  $: typeBadge = item.platform === 'ps2' ? (item.discType || 'untyped').toUpperCase() : 'PS1';
  $: gameIdBadge = item.gameId ? item.gameId : '';
  $: isUncertain = item.gameIdUncertain;

  let enrich: { artKey: string; artStatus: string; cheatStatus: string; regionMatched: boolean } | null = null;

  async function loadEnrich() {
    try {
      enrich = await api.enrichment(item.id);
    } catch {
      // silent: enrichment is best-effort
    }
  }

  onMount(() => { loadEnrich(); });

  function artBadgeClass(s: string): string {
    if (s === 'found') return 'pill-green';
    if (s === 'custom') return 'pill-purple';
    return 'pill-gray';
  }
</script>

<div class="game-card" class:list>
  <div class="tile" style="--tile-hue: {hue(item.title)}deg" title="Cover art arrives in M5">
    <span class="platform">{item.platform.toUpperCase()}</span>
    <span class="mono">{monogram(item.title)}</span>
  </div>
  <div class="meta">
    {#if renaming}
      <input
        class="field rename"
        bind:value={draftTitle}
        onkeydown={(e) => {
          if (e.key === 'Enter') saveRename();
          if (e.key === 'Escape') renaming = false;
        }}
      />
      <div class="rename-actions">
        <button class="btn-primary" disabled={busy !== ''} onclick={saveRename}>Save</button>
        <button class="btn-ghost" onclick={() => (renaming = false)}>Cancel</button>
      </div>
    {:else}
      <span class="title" title={item.title}>{item.title}</span>
      <div class="badges">
        <span class="pill pill-gray">{typeBadge}</span>
        {#if discCount > 1}
          <span class="pill pill-purple">{discCount} discs</span>
        {/if}
        <span class="pill {statusClass(item.status)}">{item.status}</span>
        {#if gameIdBadge}
          <span class="pill {isUncertain ? 'pill-warn' : 'pill-green'}" title={isUncertain ? 'GameID uncertain (mod/translation) — confirm before cheats' : 'GameID'}>
            {gameIdBadge}{isUncertain ? ' ?' : ''}
          </span>
        {/if}
        {#if enrich}
          <span class="pill {artBadgeClass(enrich.artStatus)}" title={enrich.artKey}>ART:{enrich.artStatus}</span>
          <span class="pill {enrich.cheatStatus === 'available' ? 'pill-green' : enrich.cheatStatus === 'needs_confirm' ? 'pill-warn' : 'pill-gray'}" title="Cheats">
            CHT:{enrich.cheatStatus}
          </span>
          {#if !enrich.regionMatched}
            <span class="pill pill-warn" title="Cheat region may not match disc region">region?</span>
          {/if}
        {/if}
      </div>
    {/if}
    <div class="menu-wrap">
      <button class="dots" onclick={() => (menuOpen = !menuOpen)} aria-label="Game actions">…</button>
      {#if menuOpen}
        <div class="menu">
          <button onclick={enqueue} disabled={busy !== ''}>
            Add to queue{discCount > 1 ? ` (${discCount} discs)` : ''}
          </button>
          {#if item.platform === 'ps2'}
            <button onclick={() => setType('cd')} disabled={busy !== ''}>Mark as CD</button>
            <button onclick={() => setType('dvd')} disabled={busy !== ''}>Mark as DVD</button>
          {/if}
          {#if item.platform === 'ps1'}
            <button onclick={enqueueEmber} disabled={busy !== ''} title="Copy CUE+BINs to EMBER/games without VCD conversion (beta, needs BIOS)">Add as Ember (no convert)</button>
          {/if}
          <button onclick={startRename}>Rename…</button>
        </div>
      {/if}
    </div>
  </div>
  {#if error}
    <p class="err">{error}</p>
  {/if}
</div>

<style>
  .game-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .tile {
    aspect-ratio: 1 / 1;
    display: flex;
    align-items: center;
    justify-content: center;
    position: relative;
    background:
      radial-gradient(120% 90% at 50% 0%, hsl(var(--tile-hue) 60% 32%) 0%, hsl(var(--tile-hue) 55% 12%) 70%),
      var(--bg-raised);
  }
  .platform {
    position: absolute;
    top: 8px;
    left: 8px;
    font-size: 10px;
    font-weight: 700;
    background: rgba(0, 0, 0, 0.55);
    color: #dbe7ff;
    padding: 2px 7px;
    border-radius: 6px;
    letter-spacing: 0.06em;
  }
  .mono {
    font-size: 44px;
    font-weight: 800;
    color: hsl(var(--tile-hue) 80% 78%);
    text-shadow: 0 2px 18px rgba(0, 0, 0, 0.6);
  }
  .meta {
    padding: 10px 12px 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    position: relative;
  }
  .title {
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .badges {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin-right: 30px;
  }
  .pill-danger {
    background: var(--danger-soft);
    color: var(--danger);
  }
  .pill-warn {
    background: #fff3cd;
    color: #664d03;
  }
  .pill-green {
    background: var(--success-soft, #d1f0d1);
    color: var(--success, #0a6b0a);
  }
  .pill-purple {
    background: #e8d5ff;
    color: #4c1d95;
  }
  .menu-wrap {
    position: absolute;
    right: 8px;
    bottom: 8px;
  }
  .dots {
    font-size: 18px;
    letter-spacing: 1px;
    color: var(--fg-muted);
    padding: 2px 8px;
    border-radius: 8px;
  }
  .dots:hover {
    background: var(--surface-hover);
    color: var(--fg);
  }
  .menu {
    position: absolute;
    right: 0;
    bottom: 32px;
    z-index: 10;
    min-width: 160px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: var(--shadow);
    padding: 4px;
    display: flex;
    flex-direction: column;
  }
  .menu button {
    text-align: left;
    padding: 8px 10px;
    border-radius: 6px;
    color: var(--fg);
  }
  .menu button:hover:not(:disabled) {
    background: var(--surface-hover);
  }
  .menu button:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .rename {
    padding: 6px 8px;
  }
  .rename-actions {
    display: flex;
    gap: 6px;
  }
  .rename-actions .btn-primary,
  .rename-actions .btn-ghost {
    padding: 6px 10px;
    font-size: 12px;
  }
  .err {
    color: var(--danger);
    font-size: 12px;
    margin: 0 12px 10px;
  }
  /* List mode: horizontal card. */
  .game-card.list {
    flex-direction: row;
  }
  .game-card.list .tile {
    width: 72px;
    aspect-ratio: auto;
  }
  .game-card.list .mono {
    font-size: 24px;
  }
  .game-card.list .platform {
    display: none;
  }
  .game-card.list .meta {
    flex: 1;
    flex-direction: row;
    align-items: center;
  }
  .game-card.list .menu-wrap {
    position: static;
    margin-left: auto;
  }
  .game-card.list .menu {
    bottom: auto;
    top: 32px;
  }
</style>
