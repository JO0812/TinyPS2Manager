<script lang="ts">
  import { api, type LibraryItem } from '../lib/api';

  export let item: LibraryItem;
  export let list: boolean = false;
  export let onChanged: () => void;

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
      await api.enqueue(destId, [item.id]);
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
        <span class="pill {statusClass(item.status)}">{item.status}</span>
      </div>
    {/if}
    <div class="menu-wrap">
      <button class="dots" onclick={() => (menuOpen = !menuOpen)} aria-label="Game actions">…</button>
      {#if menuOpen}
        <div class="menu">
          <button onclick={enqueue} disabled={busy !== ''}>Add to queue</button>
          {#if item.platform === 'ps2'}
            <button onclick={() => setType('cd')} disabled={busy !== ''}>Mark as CD</button>
            <button onclick={() => setType('dvd')} disabled={busy !== ''}>Mark as DVD</button>
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
  }
  .pill-danger {
    background: var(--danger-soft);
    color: var(--danger);
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
