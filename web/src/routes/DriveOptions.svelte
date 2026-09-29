<script lang="ts">
  import { onMount } from 'svelte';
  import {
    api,
    formatBytes,
    type Destination,
    type LibraryItem,
    type PreparePreview,
    type PrepareResult,
  } from '../lib/api';
  import {
    activeDest,
    activePath,
    destinations,
    loadPreflight,
    preflight,
    preflightBusy,
    preflightError,
    refreshDrives,
    setActivePath,
    volumes,
  } from '../lib/drive';

  import { browseFolder } from '../lib/dialog';

  let items: LibraryItem[] = [];
  let notice = '';
  let noticeKind: 'ok' | 'err' = 'ok';

  // Add-drive form.
  let pickedVolume = '';
  let newPath = '';

  async function refresh() {
    try {
      items = await api.library();
      await refreshDrives();
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  function pick(path: string) {
    setActivePath(path);
  }

  async function removeDrive(d: Destination) {
    if (!confirm(`Remove destination ${d.path}?\nDestinations with queued jobs cannot be removed.`)) return;
    notice = '';
    try {
      await api.deleteDestination(d.path);
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
        kind: 'folder',
      });
      newPath = '';
      pickedVolume = '';
      await refresh();
      pick(d.path);
      notice = 'Destination added.';
      noticeKind = 'ok';
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  // Picking a detected drive adds it immediately (kind=drive, fs
  // auto-detected); picking one that is already tracked just selects it.
  // "Other folder…" reveals a single path field for staging folders.
  // Filesystem override and BDM prefix live on the Selected-drive panel.
  async function onPickVolume(path: string) {
    if (path === '__other') {
      pickedVolume = path;
      return;
    }
    pickedVolume = path;
    if (!path) return;
    const existing = $destinations.find((d) => d.path === path);
    if (existing) {
      pick(existing.path);
      notice = 'Already in the list — selected.';
      noticeKind = 'ok';
      pickedVolume = '';
      return;
    }
    notice = '';
    try {
      const d = await api.createDestination({ path, kind: 'drive' });
      await refresh();
      pick(d.path);
      notice = 'Destination added.';
      noticeKind = 'ok';
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    } finally {
      pickedVolume = '';
    }
  }

  // Pending values keep the user's choice on screen while its PATCH +
  // refresh round-trips. Per-field tokens make overlap safe: only the
  // latest save of each field may clear its pending display.
  let pendingFs: string | null = null;
  let pendingPrefix: string | null = null;
  let fsBusy = false;
  let prefixBusy = false;
  let fsSeq = 0;
  let prefixSeq = 0;

  async function savePrefix(path: string, prefix: string) {
    const mine = ++prefixSeq;
    pendingPrefix = prefix;
    prefixBusy = true;
    notice = '';
    try {
      await api.patchDestination(path, { bdmPrefix: prefix });
      await refresh();
      await loadPreflight(path);
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

  async function saveFs(path: string, ov: string) {
    const mine = ++fsSeq;
    pendingFs = ov;
    fsBusy = true;
    notice = '';
    try {
      await api.patchDestination(path, { filesystemOverride: ov });
      await refresh();
      await loadPreflight(path);
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

  $: dest = $activeDest;
  // Unplugged (stored but missing) destinations sink to the bottom so live
  // drives stay on top; the row itself carries the unplugged badge.
  $: sorted = [...$destinations].sort((a, b) => Number(a.reachable === false) - Number(b.reachable === false));
  $: itemIds = items.map((i) => i.id);
  $: fsShown = pendingFs ?? dest?.fsOverride ?? '';
  $: prefixShown = pendingPrefix ?? dest?.bdmPrefix ?? '';

  function fsText(d: Destination): string {
    if (d.freeBytes < 0) return `${d.filesystem.toUpperCase()} (size unknown)`;
    const base = `${d.filesystem.toUpperCase()} (${formatBytes(d.freeBytes)} free`;
    return d.totalBytes > 0 ? `${base} of ${formatBytes(d.totalBytes)})` : `${base})`;
  }

  // ---- Prepare card: the tree preview loads itself; the loader stays an
  // explicit click (network fetches are always per-action), and one button
  // enqueues everything. No dialog — this flow needs no interruption.
  let tag: string = 'current-fan-favorite';
  let ps1Mode: 'vcd' | 'ember' = 'vcd';
  let preview: PreparePreview | null = null;
  let previewBusy = false;
  let previewError = '';
  let result: PrepareResult | null = null;
  let executeBusy = false;
  let executeError = '';

  let previewSeq = 0;

  function ps1Kind(): string | undefined {
    return ps1Mode === 'ember' ? 'copy-ps1-ember' : undefined;
  }

  async function loadTreePreview() {
    const path = $activePath;
    if (!path || itemIds.length === 0) {
      preview = null;
      return;
    }
    const seq = ++previewSeq;
    previewBusy = true;
    previewError = '';
    result = null;
    try {
      const res = (await api.prepare(path, { mode: 'preview', itemIds, kind: ps1Kind() })) as PreparePreview;
      if (seq !== previewSeq) return;
      preview = res;
    } catch (e) {
      if (seq !== previewSeq) return;
      previewError = e instanceof Error ? e.message : String(e);
      preview = null;
    } finally {
      if (seq === previewSeq) previewBusy = false;
    }
  }

  async function checkRelease() {
    const path = $activePath;
    if (!path) return;
    const seq = ++previewSeq;
    previewBusy = true;
    previewError = '';
    try {
      const res = (await api.prepare(path, {
        mode: 'preview',
        itemIds,
        riptoplTag: tag,
        kind: ps1Kind(),
      })) as PreparePreview;
      if (seq !== previewSeq) return;
      preview = res;
    } catch (e) {
      if (seq !== previewSeq) return;
      previewError = e instanceof Error ? e.message : String(e);
    } finally {
      if (seq === previewSeq) previewBusy = false;
    }
  }

  async function browse() {
    const path = await browseFolder('Choose staging folder');
    if (path) {
      newPath = path;
      notice = '';
    } else {
      notice = 'Folder picker is only available in the desktop app — type the path.';
      noticeKind = 'err';
    }
  }

  async function execute() {
    const path = $activePath;
    if (!path) return;
    executeBusy = true;
    executeError = '';
    try {
      const res = (await api.prepare(path, {
        mode: 'execute',
        itemIds,
        riptoplTag: preview?.riptopl ? tag : undefined,
        kind: ps1Kind(),
      })) as PrepareResult;
      result = res;
      preview = null;
    } catch (e) {
      executeError = e instanceof Error ? e.message : String(e);
    } finally {
      executeBusy = false;
    }
  }

  // Auto-preview when the drive or the library set changes.
  $: previewKey = `${$activePath}|${itemIds.join(',')}|${ps1Mode}`;
  $: if (previewKey) {
    void loadTreePreview();
  }

  onMount(refresh);
</script>

<h1 class="view-title">Toolbox</h1>
<p class="muted lede">One active drive, prepared in one pass — folders are created, the loader is staged when asked, then everything is enqueued. This app never formats drives.</p>

{#if notice}
  <p class="notice" class:err={noticeKind === 'err'} role={noticeKind === 'err' ? 'alert' : 'status'}>{notice}</p>
{/if}

<div class="cols">
  <div class="card drives">
    <h2>Drive</h2>
    {#if $destinations.length === 0}
      <p class="muted">None yet — add your USB stick or staging folder below.</p>
    {/if}
    {#each sorted as d}
      <div class="drive-row">
        <button class="drive" class:active={d.path === $activePath} class:unreachable={d.reachable === false} onclick={() => pick(d.path)}>
          <span class="drive-path">{d.path}</span>
          {#if d.reachable === false}
            <span class="pill pill-warn" title="Stored destination, currently unplugged — plug it back in to resume, or remove it with ✕">unplugged</span>
          {/if}
          <span class="pill {(d.fsOverride || d.filesystem) === 'unknown' ? 'pill-gray' : 'pill-green'}" title={d.fsOverride ? 'explicit override' : 'detected'}>
            {(d.fsOverride || d.filesystem).toUpperCase()}{d.fsOverride ? '*' : ''}
          </span>
          <span class="muted small">{d.kind}{d.bdmPrefix ? ` · ${d.bdmPrefix}` : ''}</span>
        </button>
        <button class="btn-ghost small danger" title="Remove destination" aria-label="Remove {d.path}" onclick={() => removeDrive(d)}>✕</button>
      </div>
    {/each}
    <h3>Add destination</h3>
    {#if $volumes.length > 0}
      <label>
        Detected drives
        <select value={pickedVolume} onchange={(e) => onPickVolume(e.currentTarget.value)} aria-label="Detected drives">
          <option value="">Choose a drive…</option>
          {#each $volumes as v}
            <option value={v.path} disabled={v.added}>
              {v.label} — {(v.filesystem || 'unknown').toUpperCase()} · {formatBytes(v.freeBytes)}{v.added ? ' (added)' : ''}
            </option>
          {/each}
          <option value="__other">Other folder…</option>
        </select>
      </label>
    {:else}
      <p class="muted small">No removable drives detected — type a staging path below.</p>
    {/if}
    {#if pickedVolume === '__other' || $volumes.length === 0}
      <div class="row">
        <input
          class="field"
          bind:value={newPath}
          placeholder="/home/you/staging"
          aria-label="New destination path"
          onkeydown={(e) => {
            if (e.key === 'Enter') addDrive();
          }}
        />
        <button class="btn-ghost" onclick={browse}>Browse…</button>
        <button class="btn-ghost" onclick={addDrive}>Add</button>
      </div>
    {/if}

    {#if dest}
      <div class="detail">
        <h3>Selected drive</h3>
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
            onchange={(e) => saveFs(dest.path, e.currentTarget.value)}
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
            onchange={(e) => savePrefix(dest.path, e.currentTarget.value)}
            placeholder="(drive root)"
          />
          {#if prefixBusy}<span class="muted small">Saving…</span>{/if}
        </label>

        <div class="preflight">
          <h3>Pre-flight checks</h3>
          <button class="btn-ghost small" onclick={() => loadPreflight($activePath)} disabled={$preflightBusy || !$activePath}>
            {$preflightBusy ? 'Checking…' : 'Re-check'}
          </button>
          {#if $preflightError}
            <p class="notice err">{$preflightError}</p>
          {/if}
          {#if $preflight}
            <ul class="checks">
              {#each $preflight.checks as c}
                <li class="check {c.status}">
                  <span class="badge {c.status}">{c.status}</span>
                  <strong>{c.name}</strong> — {c.message}
                </li>
              {/each}
            </ul>
            {#if $preflight.blocked}
              <p class="notice err">Pre-flight blocked: fix fail checks before enqueue.</p>
            {:else}
              <p class="notice">Pre-flight passed (warnings are non-blocking).</p>
            {/if}
          {/if}
        </div>
      </div>
    {/if}
  </div>

  <div class="card prepare">
    <h2>Prepare {itemIds.length} game{itemIds.length === 1 ? '' : 's'}</h2>
    {#if !dest}
      <p class="muted">Select a drive on the left first.</p>
    {:else if dest.reachable === false}
      <p class="notice err">Drive is unreachable — plug it back in first.</p>
    {:else if $preflight?.blocked}
      <p class="notice err">Fix pre-flight failures before preparing.</p>
    {:else if itemIds.length === 0}
      <p class="muted">Import games in Games first — they will all be prepared at once.</p>
    {:else if result}
      <p class="notice">Enqueued {result.jobs.length} jobs.
        {#if result.riptopl}
          Loader <strong>{result.riptopl.flavour}</strong> staged ({formatBytes(result.riptopl.elfSize)}, {result.riptopl.tag}).
        {/if}
      </p>
      <h3>First boot on the PS2</h3>
      <ol class="checklist">
        {#each result.checklist as step}
          <li>{step}</li>
        {/each}
      </ol>
      <div class="row">
        <button class="btn-ghost" onclick={() => { result = null; void loadTreePreview(); }}>Prepare again</button>
      </div>
    {:else}
      <h3>RiptOPL loader</h3>
      <div class="row">
        <select bind:value={tag} aria-label="Release track">
          <option value="current-fan-favorite">Stable snapshot (recommended)</option>
          <option value="rolling">Rolling (latest, may be unstable)</option>
        </select>
        <button class="btn-ghost" disabled={previewBusy} onclick={checkRelease}>
          {previewBusy ? 'Checking…' : 'Check release'}
        </button>
      </div>
      {#if preview?.riptopl}
        {@const loader = preview.riptopl}
        <div class="loader">
          <div><strong>{loader.tag}</strong> · {loader.asset} · {formatBytes(loader.sizeBytes)}</div>
          <div class="muted small url">{loader.url}</div>
          <div class="muted small">sha256 {loader.digest.slice(0, 16)}… · flavour order: {loader.flavours.join(', ')}</div>
        </div>
      {/if}

      <h3>PS1 handling</h3>
      <div class="row">
        <select bind:value={ps1Mode} aria-label="PS1 mode">
          <option value="vcd">POPSTARTER VCD (default, mature)</option>
          <option value="ember">Ember (beta, no convert, needs bios.bin)</option>
        </select>
        <span class="muted small">Ember keeps CUE/BIN names, needs 512 KB EMBER/bios.bin at drive root</span>
      </div>

      {#if previewError}
        <p class="notice err">{previewError}</p>
      {/if}
      {#if executeError}
        <p class="notice err">{executeError}</p>
      {/if}

      {#if preview}
        <h3>Planned tree ({preview.files.length} files, {preview.dirs.length} folders)</h3>
        {#if preview.warnings.length > 0}
          <ul class="warn">
            {#each preview.warnings as w}
              <li>{w}</li>
            {/each}
          </ul>
        {/if}
        <details>
          <summary>Show paths</summary>
          <ul class="paths">
            {#each preview.dirs as d}
              <li class="dir">▸ {d}</li>
            {/each}
            {#each preview.files as f}
              <li>{f}</li>
            {/each}
          </ul>
        </details>
      {:else if previewBusy}
        <p class="muted">Planning the drive tree…</p>
      {/if}

      <button class="btn-primary big" onclick={execute} disabled={executeBusy || previewBusy || !preview || $preflight?.blocked}>
        {executeBusy ? 'Preparing…' : 'Prepare & enqueue'}
      </button>
    {/if}
  </div>
</div>

<style>
  .view-title {
    margin: 0 0 4px;
  }
  .lede {
    margin: 0 0 16px;
    max-width: 80ch;
  }
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
  .prepare {
    padding: 20px 22px;
  }
  .drives h2,
  .prepare h2 {
    margin: 0 0 12px;
    font-size: 16px;
  }
  .drives h3,
  .prepare h3 {
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
  .drive.unreachable {
    opacity: 0.65;
    border-style: dashed;
  }
  .drive-path {
    font-weight: 600;
    word-break: break-all;
  }
  .small {
    font-size: 12px;
    min-height: 32px;
    min-width: 32px;
  }
  .row {
    display: flex;
    gap: 8px;
    margin-top: 8px;
    flex-wrap: wrap;
    align-items: center;
  }
  .row select,
  .prepare select,
  .drives select {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 8px 10px;
    color: var(--fg);
  }
  .drives label,
  .detail label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 10px 0 4px;
    color: var(--fg-muted);
    font-size: 13px;
  }
  .detail {
    margin-top: 16px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
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
  .loader {
    padding: 12px 14px;
    margin-top: 10px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: 10px;
  }
  .url {
    word-break: break-all;
  }
  .warn {
    color: var(--warning);
    margin: 0 0 8px 18px;
    padding: 0;
  }
  details {
    margin: 8px 0;
  }
  summary {
    cursor: pointer;
    color: var(--accent);
  }
  .paths {
    list-style: none;
    margin: 8px 0 0;
    padding: 12px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: 10px;
    max-height: 260px;
    overflow-y: auto;
    font-family: monospace;
    font-size: 12px;
  }
  .paths li {
    white-space: nowrap;
  }
  .paths .dir {
    color: var(--accent);
  }
  .checklist {
    margin: 0 0 8px 20px;
    padding: 0;
  }
  .checklist li {
    margin-bottom: 6px;
  }
</style>
