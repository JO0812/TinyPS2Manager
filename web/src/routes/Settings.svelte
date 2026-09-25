<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type Settings } from '../lib/api';

  let settings: Settings | null = null;
  let notice = '';
  let noticeKind: 'ok' | 'err' = 'ok';

  async function load() {
    try {
      settings = await api.settings();
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  async function save() {
    if (!settings) return;
    notice = '';
    try {
      settings = await api.saveSettings(settings);
      notice = 'Settings saved.';
      noticeKind = 'ok';
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
      noticeKind = 'err';
    }
  }

  onMount(load);
</script>

<p class="eyebrow">App</p>
<h1 class="view-title">Settings</h1>

{#if notice}
  <p class="notice" class:err={noticeKind === 'err'}>{notice}</p>
{/if}

{#if !settings}
  <p class="muted">Loading…</p>
{:else}
  <div class="card form">
    <h2>Storage</h2>
    <label>
      Split threshold (bytes)
      <input class="field" type="number" min="0" bind:value={settings.splitThreshold} />
      <span class="hint">DVD images above this split into USBExtreme sets on FAT32. Default 4294967295 (4 GiB − 1).</span>
    </label>
    <label>
      Staging directory
      <input class="field" bind:value={settings.stagingDir} placeholder="(system temp)" />
    </label>
    <label>
      Default BDM prefix
      <input class="field" bind:value={settings.bdmPrefixDefault} placeholder="(drive root)" />
    </label>
    <label>
      Default filesystem
      <select bind:value={settings.filesystemDefault}>
        <option value="fat32">FAT32 (safe default)</option>
        <option value="exfat">exFAT</option>
      </select>
    </label>

    <button class="btn-primary" onclick={save}>Save settings</button>
  </div>
{/if}

<style>
  .view-title {
    margin-bottom: 16px;
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
  .form {
    padding: 22px 24px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    max-width: 560px;
  }
  .form h2 {
    font-size: 15px;
    margin: 8px 0 0;
  }
  .form h2:first-child {
    margin-top: 0;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    color: var(--fg-muted);
    font-size: 13px;
  }
  label select {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 10px 12px;
    color: var(--fg);
  }
  .hint {
    font-size: 12px;
    color: var(--fg-faint);
  }
</style>
