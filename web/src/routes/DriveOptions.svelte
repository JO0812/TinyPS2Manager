<script lang="ts">
  import { onMount } from 'svelte';
  import { api, formatBytes, type Destination, type LibraryItem, type Volume } from '../lib/api';
  import PrepareDialog from '../components/PrepareDialog.svelte';

  let destinations: Destination[] = [];
  let volumes: Volume[] = [];
  let selectedId = 0;
  let items: LibraryItem[] = [];
  let showPrepare = false;
  let notice = '';
  let noticeKind: 'ok' | 'err' = 'ok';

  // Add-drive form.
  let pickedVolume = '';
  let newPath = '';
  let newKind = 'folder';
  let newFs = '';
  let newPrefix = '';

  async function refresh() {
    try {
      const [dests, libs, vols] = await Promise.all([api.destinations(), api.library(), api.volumes()]);
      destinations = dests;
      items = libs;
      volumes = vols;
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

  async function removeDrive(d: Destination) {
    if (!confirm(`Remove destination ${d.path}?\nDestinations with queued jobs cannot be removed.`)) return;
    notice = '';
    try {
      await api.deleteDestination(d.id);
      notice = 'Destination removed.';
      noticeKind = 'ok';
      await refresh();
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
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
      pickedVolume = '';
      await refresh();
      pick(d.id);
      notice = 'Destination added.';
      noticeKind = 'ok';
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  // Picking a detected drive fills the form (kind=drive); picking one that
  // is already tracked just selects it. Nothing is created until Add.
  function onPickVolume(path: string) {
    pickedVolume = path;
    if (!path) return;
    const existing = destinations.find((d) => d.path === path);
    if (existing) {
      pick(existing.id);
      notice = 'Already in the list — selected.';
      noticeKind = 'ok';
      return;
    }
    newPath = path;
    newKind = 'drive';
    newFs = '';
  }

  // Pending values keep the user's choice on screen while its PATCH +
  // refresh round-trips; without them the controls snap back to the stale
  // server value on every unrelated re-render and rapid changes pile up.
  // Per-field tokens make overlap safe: only the latest save of each field
  // may clear its pending display, so an older save resolving late can't
  // revert a newer choice.
  let pendingFs: string | null = null;
  let pendingPrefix: string | null = null;
  let fsBusy = false;
  let prefixBusy = false;
  let fsSeq = 0;
  let prefixSeq = 0;

  async function savePrefix(id: number, prefix: string) {
    const mine = ++prefixSeq;
    pendingPrefix = prefix;
    prefixBusy = true;
    notice = '';
    try {
      await api.patchDestination(id, { bdmPrefix: prefix });
      await refresh();
      await loadPreflight(id);
    } catch (e) {
      if (mine !== prefixSeq) return;
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    } finally {
      if (mine === prefixSeq) {
        pendingPrefix = null;
        prefixBusy = false;
      }
    }
  }

  async function saveFs(id: number, ov: string) {
    const mine = ++fsSeq;
    pendingFs = ov;
    fsBusy = true;
    notice = '';
    try {
      await api.patchDestination(id, { filesystemOverride: ov });
      await refresh();
      await loadPreflight(id);
    } catch (e) {
      if (mine !== fsSeq) return;
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    } finally {
      if (mine === fsSeq) {
        pendingFs = null;
        fsBusy = false;
      }
    }
  }

  $: dest = destinations.find((d) => d.id === selectedId);
  $: itemIds = items.map((i) => i.id);
  // What the controls show: the in-flight choice wins over the last
  // server snapshot so edits never visibly revert mid-save.
  $: fsShown = pendingFs ?? dest?.fsOverride ?? '';
  $: prefixShown = pendingPrefix ?? dest?.bdmPrefix ?? '';

  function fsText(d: Destination): string {
    if (d.freeBytes < 0) return `${d.filesystem.toUpperCase()} (size unknown)`;
    const base = `${d.filesystem.toUpperCase()} (${formatBytes(d.freeBytes)} free`;
    return d.totalBytes > 0 ? `${base} of ${formatBytes(d.totalBytes)})` : `${base})`;
  }

  let preflight: { checks: { name: string; status: string; message: string }[]; blocked: boolean } | null = null;
  let preflightBusy = false;
  let preflightError = '';

  // Sequence guard: overlapping loads resolve in any order; only the
  // latest may paint. Without it a slow earlier response clobbers newer
  // state after rapid dropdown changes.
  let preflightSeq = 0;

  async function loadPreflight(id: number) {
    const seq = ++preflightSeq;
    preflightBusy = true;
    preflightError = '';
    try {
      const res = await api.preflight(id);
      if (seq !== preflightSeq) return; // stale: a newer load is in flight
      preflight = res;
    } catch (e) {
      if (seq !== preflightSeq) return;
      preflightError = e instanceof Error ? e.message : String(e);
      preflight = null;
    } finally {
      if (seq === preflightSeq) preflightBusy = false;
    }
  }

  // Auto-load only when the SELECTION changes. destinations is replaced on
  // every refresh (new object identities), so keying off `dest` refired a
  // preflight fetch after every save — piling requests on slow devices.
  $: if (selectedId) {
    void loadPreflight(selectedId);
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
      <div class="drive-row">
        <button class="drive" class:active={d.id === selectedId} onclick={() => pick(d.id)}>
          <span class="drive-path">{d.path}</span>
          <span class="pill {(d.fsOverride || d.filesystem) === 'unknown' ? 'pill-gray' : 'pill-green'}" title={d.fsOverride ? 'explicit override' : 'detected'}>
            {(d.fsOverride || d.filesystem).toUpperCase()}{d.fsOverride ? '*' : ''}
          </span>
          <span class="muted small">{d.kind}{d.bdmPrefix ? ` · ${d.bdmPrefix}` : ''}</span>
        </button>
        <button class="btn-ghost small danger" title="Remove destination" aria-label="Remove {d.path}" onclick={() => removeDrive(d)}>✕</button>
      </div>
    {/each}
    <h3>Add destination</h3>
    {#if volumes.length > 0}
      <label>
        Detected drives
        <select value={pickedVolume} onchange={(e) => onPickVolume(e.currentTarget.value)} aria-label="Detected drives">
          <option value="">Choose a drive…</option>
          {#each volumes as v}
            <option value={v.path} disabled={v.added}>
              {v.label} — {(v.filesystem || 'unknown').toUpperCase()} · {formatBytes(v.freeBytes)}{v.added ? ' (added)' : ''}
            </option>
          {/each}
        </select>
      </label>
    {:else}
      <p class="muted small">No removable drives detected — type a path below.</p>
    {/if}
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
      {#if dest.reachable === false}
        <p class="notice err">Path is missing — drive unplugged? Plug it back in; the queue resumes on its own.</p>
      {/if}
      <dl>
        <div><dt>Path</dt><dd>{dest.path}</dd></div>
        <div><dt>Filesystem</dt><dd>{fsText(dest)}</dd></div>
        <div><dt>Kind</dt><dd>{dest.kind}</dd></div>
      </dl>
      <label>
        Filesystem override
        <select
          value={fsShown}
          aria-busy={fsBusy}
          onchange={(e) => saveFs(dest.id, e.currentTarget.value)}
        >
          <option value="">Auto-detect</option>
          <option value="fat32">FAT32 (safe default)</option>
          <option value="exfat">exFAT</option>
        </select>
        {#if fsBusy}<span class="muted small">Saving…</span>{/if}
      </label>
      <label>
        BDM prefix
        <input
          class="field"
          value={prefixShown}
          aria-busy={prefixBusy}
          onchange={(e) => savePrefix(dest.id, e.currentTarget.value)}
          placeholder="(drive root)"
        />
        {#if prefixBusy}<span class="muted small">Saving…</span>{/if}
      </label>

      <div class="preflight">
        <h3>Pre-flight checks</h3>
        <button class="btn-ghost small" onclick={() => loadPreflight(selectedId)} disabled={preflightBusy || !selectedId}>
          {preflightBusy ? 'Checking…' : 'Re-check'}
        </button>
        {#if preflightError}
          <p class="notice err">{preflightError}</p>
        {/if}
        {#if preflight}
          <ul class="checks">
            {#each preflight.checks as c}
              <li class="check {c.status}">
                <span class="badge {c.status}">{c.status}</span>
                <strong>{c.name}</strong> — {c.message}
              </li>
            {/each}
          </ul>
          {#if preflight.blocked}
            <p class="notice err">Pre-flight blocked: fix fail checks before enqueue.</p>
          {:else}
            <p class="notice">Pre-flight passed (warnings are non-blocking).</p>
          {/if}
        {/if}
      </div>

      <button class="btn-primary big" onclick={() => (showPrepare = true)} disabled={items.length === 0 || preflight?.blocked || dest.reachable === false}>
        Prepare external drive
      </button>
      {#if dest.reachable === false}
        <p class="muted small">Drive is unreachable — plug it back in first.</p>
      {:else if preflight?.blocked}
        <p class="muted small">Fix pre-flight failures before preparing.</p>
      {:else if items.length === 0}
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
  .drive-row {
    display: flex;
    gap: 8px;
    align-items: stretch;
    margin-bottom: 8px;
  }
  .drive-row .drive {
    flex: 1;
    margin-bottom: 0;
  }
  .drive-row .danger {
    color: var(--danger);
    flex-shrink: 0;
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
  .detail select,
  .drives select {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 8px 10px;
    color: var(--fg);
  }
  .drives label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 10px 0 4px;
    color: var(--fg-muted);
    font-size: 13px;
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
  .preflight {
    margin-top: 16px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }
  .preflight h3 {
    margin: 0 0 8px;
    font-size: 14px;
  }
  .checks {
    list-style: none;
    margin: 8px 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .check {
    display: flex;
    gap: 8px;
    align-items: baseline;
    font-size: 12px;
    padding: 6px 8px;
    border-radius: 8px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
  }
  .check.fail {
    border-color: var(--danger);
    background: var(--danger-soft);
  }
  .check.warn {
    border-color: var(--warning);
    background: rgba(251, 191, 36, 0.1);
  }
  .badge {
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    padding: 2px 6px;
    border-radius: 6px;
    min-width: 36px;
    text-align: center;
  }
  .badge.pass {
    background: var(--success-soft);
    color: var(--success);
  }
  .badge.fail {
    background: var(--danger-soft);
    color: var(--danger);
  }
  .badge.warn {
    background: rgba(251, 191, 36, 0.16);
    color: var(--warning);
  }
</style>
