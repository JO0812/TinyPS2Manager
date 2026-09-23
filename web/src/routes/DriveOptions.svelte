<script lang="ts">
  import { onMount } from 'svelte';
  import { api, formatBytes, type Destination, type LibraryItem } from '../lib/api';
  import PrepareDialog from '../components/PrepareDialog.svelte';

  let destinations: Destination[] = [];
  let selectedId = 0;
  let items: LibraryItem[] = [];
  let showPrepare = false;
  let notice = '';
  let noticeKind: 'ok' | 'err' = 'ok';

  // Add-drive form.
  let newPath = '';
  let newKind = 'folder';
  let newFs = '';
  let newPrefix = '';

  async function refresh() {
    try {
      const [dests, libs] = await Promise.all([api.destinations(), api.library()]);
      destinations = dests;
      items = libs;
      if (!dests.some((d) => d.id === selectedId)) {
        selectedId = dests[0]?.id ?? 0;
        localStorage.setItem('oplbm.destId', String(selectedId));
      }
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  function pick(id: number) {
    selectedId = id;
    localStorage.setItem('oplbm.destId', String(id));
  }

  async function addDrive() {
    if (!newPath.trim()) return;
    notice = '';
    try {
      const d = await api.createDestination({
        path: newPath.trim(),
        kind: newKind,
        filesystemOverride: newFs || undefined,
        bdmPrefix: newPrefix.trim() || undefined,
      });
      newPath = '';
      newPrefix = '';
      await refresh();
      pick(d.id);
      notice = 'Destination added.';
      noticeKind = 'ok';
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  async function savePrefix(dest: Destination, prefix: string) {
    try {
      await api.patchDestination(dest.id, { bdmPrefix: prefix });
      await refresh();
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  async function saveFs(dest: Destination, ov: string) {
    try {
      await api.patchDestination(dest.id, { filesystemOverride: ov });
      await refresh();
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  $: dest = destinations.find((d) => d.id === selectedId);
  $: itemIds = items.map((i) => i.id);

  function fsText(d: Destination): string {
    const base = `${d.filesystem.toUpperCase()} (${formatBytes(d.freeBytes)} free`;
    return d.totalBytes > 0 ? `${base} of ${formatBytes(d.totalBytes)})` : `${base})`;
  }

  onMount(() => {
    selectedId = Number(localStorage.getItem('oplbm.destId') || '0');
    refresh();
  });
</script>

<p class="eyebrow">Targets</p>
<h1 class="view-title">Drive Options</h1>
<p class="muted">The app writes to mounted drives and folders — it never formats or partitions (destructive work stays with your OS tools).</p>

{#if notice}
  <p class="notice" class:err={noticeKind === 'err'}>{notice}</p>
{/if}

<div class="cols">
  <div class="card drives">
    <h2>Destinations</h2>
    {#if destinations.length === 0}
      <p class="muted">None yet — add your USB stick or staging folder below.</p>
    {/if}
    {#each destinations as d}
      <button class="drive" class:active={d.id === selectedId} onclick={() => pick(d.id)}>
        <span class="drive-path">{d.path}</span>
        <span class="pill {(d.fsOverride || d.filesystem) === 'unknown' ? 'pill-gray' : 'pill-green'}" title={d.fsOverride ? 'explicit override' : 'detected'}>
          {(d.fsOverride || d.filesystem).toUpperCase()}{d.fsOverride ? '*' : ''}
        </span>
        <span class="muted small">{d.kind}{d.bdmPrefix ? ` · ${d.bdmPrefix}` : ''}</span>
      </button>
    {/each}
    <h3>Add destination</h3>
    <input class="field" bind:value={newPath} placeholder="/media/usb  or  /home/you/staging" aria-label="New destination path" />
    <div class="row">
      <select bind:value={newKind} aria-label="Kind">
        <option value="folder">Folder</option>
        <option value="drive">Drive</option>
      </select>
      <select bind:value={newFs} aria-label="Filesystem override">
        <option value="">Auto-detect fs</option>
        <option value="fat32">FAT32</option>
        <option value="exfat">exFAT</option>
      </select>
      <input class="field" bind:value={newPrefix} placeholder="BDM prefix (optional)" aria-label="BDM prefix" />
      <button class="btn-ghost" onclick={addDrive}>Add</button>
    </div>
  </div>

  <div class="card detail">
    {#if dest}
      <h2>Selected drive</h2>
      <dl>
        <div><dt>Path</dt><dd>{dest.path}</dd></div>
        <div><dt>Filesystem</dt><dd>{fsText(dest)}</dd></div>
        <div><dt>Kind</dt><dd>{dest.kind}</dd></div>
      </dl>
      <label>
        Filesystem override
        <select value={dest.fsOverride} onchange={(e) => saveFs(dest, e.currentTarget.value)}>
          <option value="">Auto-detect</option>
          <option value="fat32">FAT32 (safe default)</option>
          <option value="exfat">exFAT</option>
        </select>
      </label>
      <label>
        BDM prefix
        <input class="field" value={dest.bdmPrefix} onchange={(e) => savePrefix(dest, e.currentTarget.value)} placeholder="(drive root)" />
      </label>
      <button class="btn-primary big" onclick={() => (showPrepare = true)} disabled={items.length === 0}>
        Prepare external drive
      </button>
      {#if items.length === 0}
        <p class="muted small">Import games in the Library first.</p>
      {/if}
    {:else}
      <p class="muted">Select a destination to inspect it.</p>
    {/if}
  </div>
</div>

{#if showPrepare && dest}
  <PrepareDialog {dest} {itemIds} onClose={() => (showPrepare = false)} />
{/if}

<style>
  .cols {
    display: grid;
    grid-template-columns: minmax(280px, 380px) 1fr;
    gap: 16px;
    margin-top: 16px;
  }
  @media (max-width: 900px) {
    .cols {
      grid-template-columns: 1fr;
    }
  }
  .drives,
  .detail {
    padding: 20px 22px;
  }
  .drives h2,
  .detail h2 {
    margin: 0 0 12px;
    font-size: 16px;
  }
  .drives h3 {
    margin: 18px 0 8px;
    font-size: 14px;
  }
  .drive {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    width: 100%;
    text-align: left;
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: 10px;
    margin-bottom: 8px;
  }
  .drive:hover {
    background: var(--surface-hover);
  }
  .drive.active {
    border-color: var(--accent-border);
    background: var(--accent-soft);
  }
  .drive-path {
    font-weight: 600;
    word-break: break-all;
  }
  .small {
    font-size: 12px;
  }
  .row {
    display: flex;
    gap: 8px;
    margin-top: 8px;
    flex-wrap: wrap;
  }
  .row select,
  .detail select {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 8px 10px;
    color: var(--fg);
  }
  .detail dl {
    margin: 0 0 12px;
  }
  .detail dl > div {
    display: flex;
    gap: 12px;
    padding: 6px 0;
    border-bottom: 1px solid var(--border);
  }
  .detail dt {
    color: var(--fg-muted);
    min-width: 110px;
  }
  .detail dd {
    margin: 0;
    word-break: break-all;
  }
  .detail label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 10px 0;
    color: var(--fg-muted);
    font-size: 13px;
  }
  .big {
    margin-top: 12px;
    width: 100%;
    padding: 12px;
  }
  .notice {
    padding: 10px 14px;
    border-radius: 10px;
    background: var(--success-soft);
    color: var(--success);
  }
  .notice.err {
    background: var(--danger-soft);
    color: var(--danger);
  }
</style>
