<script lang="ts">
  import { api, formatBytes, type Destination, type PreparePreview, type PrepareResult } from '../lib/api';

  export let dest: Destination;
  export let itemIds: number[];
  export let onClose: () => void;

  let tag: string = 'current-fan-favorite';
  let ps1Mode: 'vcd' | 'ember' = 'vcd';
  let preview: PreparePreview | null = null;
  let result: PrepareResult | null = null;
  let busy = '';
  let error = '';

  async function loadPreview(withLoader: boolean) {
    busy = 'preview';
    error = '';
    result = null;
    try {
      const res = await api.prepare(dest.id, {
        mode: 'preview',
        itemIds,
        riptoplTag: withLoader ? tag : undefined,
        kind: ps1Mode === 'ember' ? 'copy-ps1-ember' : undefined,
      });
      preview = res as PreparePreview;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = '';
    }
  }

  async function execute() {
    busy = 'execute';
    error = '';
    try {
      const res = (await api.prepare(dest.id, {
        mode: 'execute',
        itemIds,
        riptoplTag: preview?.riptopl ? tag : undefined,
        kind: ps1Mode === 'ember' ? 'copy-ps1-ember' : undefined,
      })) as PrepareResult;
      result = res;
      preview = null;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = '';
    }
  }

  $: loader = preview?.riptopl ?? null;
  $: shown = result
    ? { dirs: result.dirs, files: result.files, warnings: [] as string[] }
    : preview
      ? { dirs: preview.dirs, files: preview.files, warnings: preview.warnings }
      : null;
</script>

<div class="overlay" onclick={onClose} onkeydown={(e) => e.key === 'Escape' && onClose()} role="presentation">
  <div
    class="dialog card"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
    role="dialog"
    tabindex="-1"
    aria-label="Prepare external drive"
  >
    <h2>Prepare external drive</h2>
    <p class="muted">
      Target <strong>{dest.path}</strong> · {itemIds.length} librar{itemIds.length === 1 ? 'y item' : 'y items'}.
      Folders are created, the loader is staged when asked, then everything is enqueued — the
      Activity view runs the queue one file at a time. This app never formats drives.
    </p>

    <h3>RiptOPL loader</h3>
    <div class="row">
      <select bind:value={tag} aria-label="Release track">
        <option value="current-fan-favorite">Stable snapshot (recommended)</option>
        <option value="rolling">Rolling (latest, may be unstable)</option>
      </select>
      <button class="btn-ghost" disabled={busy !== ''} onclick={() => loadPreview(true)}>
        {busy === 'preview' ? 'Checking…' : 'Check release'}
      </button>
      <button class="btn-ghost" disabled={busy !== ''} onclick={() => loadPreview(false)}>Tree only</button>
    </div>
    {#if loader}
      <div class="card loader">
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

    {#if error}
      <p class="notice err">{error}</p>
    {/if}

    {#if shown}
      <h3>Planned tree ({shown.files.length} files, {shown.dirs.length} folders)</h3>
      {#if shown.warnings.length > 0}
        <ul class="warn">
          {#each shown.warnings as w}
            <li>{w}</li>
          {/each}
        </ul>
      {/if}
      <details>
        <summary>Show paths</summary>
        <ul class="paths">
          {#each shown.dirs as d}
            <li class="dir">▸ {d}</li>
          {/each}
          {#each shown.files as f}
            <li>{f}</li>
          {/each}
        </ul>
      </details>
    {/if}

    {#if result}
      <h3>Done</h3>
      <p>
        Enqueued {result.jobs.length} jobs.
        {#if result.riptopl}
          Loader <strong>{result.riptopl.flavour}</strong> staged ({formatBytes(result.riptopl.elfSize)},
          {result.riptopl.tag}).
        {/if}
      </p>
      <h3>First boot on the PS2</h3>
      <ol class="checklist">
        {#each result.checklist as step}
          <li>{step}</li>
        {/each}
      </ol>
      <div class="row">
        <button class="btn-primary" onclick={onClose}>Close</button>
      </div>
    {:else}
      <div class="row">
        <button class="btn-primary" disabled={busy !== '' || !shown} onclick={execute}>
          {busy === 'execute' ? 'Preparing…' : 'Prepare & enqueue'}
        </button>
        <button class="btn-ghost" onclick={onClose}>Cancel</button>
      </div>
    {/if}
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: 48px 16px;
    z-index: 50;
    overflow-y: auto;
  }
  .dialog {
    max-width: 640px;
    width: 100%;
    padding: 24px 28px;
  }
  .dialog h2 {
    margin: 0 0 8px;
  }
  .dialog h3 {
    margin: 18px 0 8px;
    font-size: 15px;
  }
  .row {
    display: flex;
    gap: 10px;
    margin: 10px 0;
    flex-wrap: wrap;
  }
  .row select {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 8px 12px;
    color: var(--fg);
  }
  .loader {
    padding: 12px 14px;
    margin-top: 10px;
  }
  .url {
    word-break: break-all;
  }
  .notice.err {
    background: var(--danger-soft);
    color: var(--danger);
    padding: 10px 14px;
    border-radius: 10px;
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
