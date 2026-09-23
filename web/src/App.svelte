<script lang="ts">
  import { onMount } from 'svelte';
  import './app.css';
  import { api } from './lib/api';
  import { initTheme } from './lib/theme';
  import Library from './routes/Library.svelte';
  import Activity from './routes/Activity.svelte';
  import DriveOptions from './routes/DriveOptions.svelte';
  import Settings from './routes/Settings.svelte';

  type View = 'library' | 'activity' | 'drive' | 'settings';
  let view: View = 'library';
  let apiOk: boolean | null = null;

  const nav: { id: View; label: string; icon: string }[] = [
    { id: 'library', label: 'Library', icon: '▦' },
    { id: 'activity', label: 'Activity', icon: '⤓' },
    { id: 'drive', label: 'Drive Options', icon: '⚙' },
  ];

  onMount(async () => {
    try {
      await initTheme();
      await api.library();
      apiOk = true;
    } catch {
      apiOk = false;
    }
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
          <span class="nav-icon">{item.icon}</span>
          <span class="nav-label">{item.label}</span>
        </button>
      {/each}
    </nav>
    <div class="sidebar-foot">
      <button onclick={() => (view = 'settings')} title="Settings" class:active={view === 'settings'}>
        <span class="nav-icon">⚙</span>
      </button>
      <span class="api-dot" class:ok={apiOk === true} class:bad={apiOk === false} title={apiOk === null ? 'Connecting…' : apiOk ? 'API connected' : 'API unreachable — is oplbm serve running?'}></span>
    </div>
  </aside>

  <main>
    {#if view === 'library'}
      <Library onGoDrive={() => (view = 'drive')} />
    {:else if view === 'activity'}
      <Activity />
    {:else if view === 'drive'}
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
  .nav-icon {
    font-size: 20px;
    line-height: 1;
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
    padding: 28px 36px 60px;
    max-width: 1500px;
  }
</style>
