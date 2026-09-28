<script lang="ts">
  import { onMount } from 'svelte';
  import './app.css';
  import { api, formatBytes } from './lib/api';
  import { initTheme } from './lib/theme';
  import {
    activeDest,
    activePath,
    destinations,
    drivesError,
    driveSpace,
    refreshDrives,
    setActivePath,
  } from './lib/drive';
  import Library from './routes/Library.svelte';
  import Activity from './routes/Activity.svelte';
  import DriveOptions from './routes/DriveOptions.svelte';
  import Settings from './routes/Settings.svelte';

  type View = 'games' | 'queue' | 'toolbox' | 'settings';
  let view: View = 'games';
  let apiOk: boolean | null = null;

  const nav: { id: View; label: string; path: string }[] = [
    {
      id: 'games',
      label: 'Games',
      path: 'M4 4h7v7H4zM13 4h7v4h-7zM13 11h7v9h-7zM4 14h7v6H4z',
    },
    {
      id: 'queue',
      label: 'Queue',
      path: 'M12 4v11m0 0 4-4m-4 4-4-4M5 20h14',
    },
    {
      id: 'toolbox',
      label: 'Toolbox',
      path: 'M14.5 6.5a4 4 0 0 0-5.6 5L4 16.4V20h3.6l4.9-4.9a4 4 0 0 0 5-5.6l-2.8 2.8-2.4-.7-.7-2.4z',
    },
  ];

  $: dest = $activeDest;
  $: usedPct =
    dest && dest.totalBytes > 0 && dest.freeBytes >= 0
      ? Math.min(100, Math.max(0, ((dest.totalBytes - dest.freeBytes) / dest.totalBytes) * 100))
      : 0;

  onMount(async () => {
    try {
      await initTheme();
      await api.library();
      apiOk = true;
    } catch {
      apiOk = false;
    }
    refreshDrives();
  });
</script>

<div class="shell">
  <aside class="sidebar">
    <div class="logo" title="OPL Backup Manager">
      <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="#fff" stroke-width="2">
        <rect x="2" y="7" width="20" height="11" rx="5" />
        <path d="M7 11v4M5 13h4" stroke-linecap="round" />
        <circle cx="15.5" cy="12" r="1" fill="#fff" />
        <circle cx="18" cy="14" r="1" fill="#fff" />
      </svg>
    </div>
    <nav>
      {#each nav as item}
        <button class:active={view === item.id} onclick={() => (view = item.id)} title={item.label}>
          <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d={item.path} />
          </svg>
          <span class="nav-label">{item.label}</span>
        </button>
      {/each}
    </nav>
    <div class="sidebar-foot">
      <button onclick={() => (view = 'settings')} title="Settings" class:active={view === 'settings'}>
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="3.2" />
          <path d="M19 12a7 7 0 0 0-.14-1.4l2-1.55-2-3.46-2.36.95a7 7 0 0 0-2.42-1.4L13.7 2.6h-3.4l-.38 2.54a7 7 0 0 0-2.42 1.4l-2.36-.95-2 3.46 2 1.55a7 7 0 0 0 0 2.8l-2 1.55 2 3.46 2.36-.95a7 7 0 0 0 2.42 1.4l.38 2.54h3.4l.38-2.54a7 7 0 0 0 2.42-1.4l2.36.95 2-3.46-2-1.55c.1-.46.14-.93.14-1.4Z" />
        </svg>
      </button>
      <span class="api-dot" class:ok={apiOk === true} class:bad={apiOk === false} title={apiOk === null ? 'Connecting…' : apiOk ? 'API connected' : 'API unreachable — is oplbm serve running?'}></span>
    </div>
  </aside>

  <main>
    <div class="drivebar" role="status">
      <span class="drive-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.8">
          <rect x="3" y="13" width="18" height="7" rx="2" />
          <path d="M7 13V9a5 5 0 0 1 10 0v4" stroke-linecap="round" />
        </svg>
      </span>
      {#if dest}
        <button class="drive-name" onclick={() => (view = 'toolbox')} title="Open Toolbox to manage drives">
          {dest.path.split('/').filter(Boolean).pop() ?? dest.path}
          {#if dest.reachable === false}<span class="pill pill-warn">unplugged</span>{/if}
        </button>
        <span class="muted small drive-path">{dest.path}</span>
        {#if $destinations.length > 1}
          <select
            class="drive-switch"
            value={$activePath}
            onchange={(e) => setActivePath(e.currentTarget.value)}
            aria-label="Active drive"
          >
            {#each $destinations as d}
              <option value={d.path}>{d.path}{d.reachable === false ? ' (unplugged)' : ''}</option>
            {/each}
          </select>
        {/if}
        <span class="muted small drive-space">
          {#if dest.freeBytes < 0}size unknown{:else}{formatBytes(dest.freeBytes)} free{/if}
          {#if $driveSpace} · {$driveSpace}{/if}
        </span>
        {#if dest.totalBytes > 0 && dest.freeBytes >= 0}
          <span class="mini-track"><span class="mini-fill" style="width: {usedPct}%"></span></span>
        {/if}
      {:else if $drivesError}
        <span class="muted small">Drive list unavailable — { $drivesError }</span>
        <button class="btn-ghost small" onclick={() => (view = 'toolbox')}>Open Toolbox</button>
      {:else}
        <span class="muted small">No drive selected</span>
        <button class="btn-ghost small" onclick={() => (view = 'toolbox')}>Open Toolbox</button>
      {/if}
    </div>

    {#if view === 'games'}
      <Library onGoToolbox={() => (view = 'toolbox')} />
    {:else if view === 'queue'}
      <Activity />
    {:else if view === 'toolbox'}
      <DriveOptions />
    {:else}
      <Settings />
    {/if}
  </main>
</div>

<style>
  .shell {
    display: flex;
    min-height: 100vh;
  }
  .sidebar {
    width: 96px;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 20px 0;
    gap: 8px;
    border-right: 1px solid var(--border);
    background: var(--bg-raised);
    position: sticky;
    top: 0;
    height: 100vh;
  }
  .logo {
    width: 44px;
    height: 44px;
    border-radius: 14px;
    background: var(--accent);
    display: flex;
    align-items: center;
    justify-content: center;
    margin-bottom: 16px;
  }
  nav {
    display: flex;
    flex-direction: column;
    gap: 4px;
    width: 100%;
    align-items: center;
  }
  nav button,
  .sidebar-foot button {
    width: 76px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 10px 4px;
    border-radius: 12px;
    color: var(--fg-muted);
    font-size: 11px;
  }
  nav button:hover,
  .sidebar-foot button:hover {
    background: var(--surface-hover);
    color: var(--fg);
  }
  nav button.active,
  .sidebar-foot button.active {
    background: var(--accent-soft);
    color: var(--fg);
  }
  .sidebar-foot {
    margin-top: auto;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
  }
  .api-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--fg-faint);
  }
  .api-dot.ok {
    background: var(--success);
  }
  .api-dot.bad {
    background: var(--danger);
  }
  main {
    flex: 1;
    min-width: 0;
    padding: 20px 36px 60px;
    max-width: 1500px;
  }
  .drivebar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 16px;
    margin-bottom: 20px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    flex-wrap: wrap;
  }
  .drive-icon {
    color: var(--accent);
    display: flex;
  }
  .drive-name {
    font-weight: 700;
    font-size: 15px;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 2px 4px;
    border-radius: 6px;
  }
  .drive-name:hover {
    background: var(--surface-hover);
  }
  .drive-path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 320px;
  }
  .drive-switch {
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 6px 8px;
    color: var(--fg);
    font-size: 12px;
    max-width: 260px;
  }
  .drive-space {
    margin-left: auto;
  }
  .mini-track {
    width: 120px;
    height: 6px;
    border-radius: 3px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
    overflow: hidden;
  }
  .mini-fill {
    display: block;
    height: 100%;
    background: var(--accent);
  }
  .small {
    font-size: 12px;
  }
</style>
