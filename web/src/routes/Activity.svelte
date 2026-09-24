<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, subscribeQueueEvents, formatBytes, type Job, type LibraryItem } from '../lib/api';

  let jobs: Job[] = [];
  let titles: Record<number, string> = {};
  let pausedAll = false;
  let notice = '';
  let loading = true;
  let stopEvents: (() => void) | null = null;

  async function refresh() {
    try {
      const [js, libs] = await Promise.all([api.queue(), api.library()]);
      jobs = js;
      titles = Object.fromEntries(libs.map((l: LibraryItem) => [l.id, l.title]));
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  function mergeEvent(e: { jobId: number; phase: string; bytesDone: number; bytesTotal: number; etaSec: number; message: string }) {
    if (e.message === 'removed') {
      jobs = jobs.filter((j) => j.id !== e.jobId);
      return;
    }
    jobs = jobs.map((j) =>
      j.id === e.jobId ? { ...j, phase: e.phase, bytesDone: e.bytesDone, bytesTotal: e.bytesTotal } : j,
    );
    // Status flips (pending→running→done) arrive via refresh; poll lightly.
  }

  async function act(id: number, action: string) {
    notice = '';
    try {
      await api.jobAction(id, { action });
      await refresh();
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
    }
  }

  async function move(id: number, dir: -1 | 1) {
    const idx = jobs.findIndex((j) => j.id === id);
    const to = idx + dir;
    if (idx < 0 || to < 0 || to >= jobs.length) return;
    notice = '';
    try {
      await api.jobAction(id, { order: jobs[to].order });
      await refresh();
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
    }
  }

  async function toggleAll() {
    try {
      const res = pausedAll ? await api.resumeAll() : await api.pauseAll();
      pausedAll = res.paused;
    } catch (e) {
      notice = e instanceof Error ? e.message : String(e);
    }
  }

  function pct(j: Job): number {
    if (j.bytesTotal <= 0) return 0;
    return Math.min(100, (j.bytesDone / j.bytesTotal) * 100);
  }

  function eta(j: Job): string {
    if (j.status === 'done') return 'done';
    if (j.status !== 'running') return j.status;
    return j.phase ? j.phase + '…' : 'running…';
  }

  function attemptLabel(j: Job): string {
    if (j.attempts === 0) return '';
    return ` · ${j.attempts} attempt${j.attempts === 1 ? '' : 's'}`;
  }

  function statusClass(s: string): string {
    if (s === 'done') return 'pill-green';
    if (s === 'error') return 'pill-danger';
    if (s === 'running') return 'pill-purple';
    return 'pill-gray';
  }

  onMount(() => {
    refresh();
    stopEvents = subscribeQueueEvents(mergeEvent);
    const poll = setInterval(refresh, 5000);
    return () => clearInterval(poll);
  });

  onDestroy(() => stopEvents?.());
</script>

<p class="eyebrow">Transfers</p>
<div class="title-row">
  <h1 class="view-title">Activity</h1>
  <button class="btn-ghost" onclick={toggleAll}>{pausedAll ? 'Resume all' : 'Pause all'}</button>
</div>

<p class="notice">Transfers run one at a time to prevent disc fragmentation on your OPL device.</p>

{#if notice}
  <p class="notice err">{notice}</p>
{/if}

{#if loading}
  <p class="muted">Loading…</p>
{:else if jobs.length === 0}
  <div class="card empty">
    <p>Queue is empty. Add games from the Library or prepare a drive to fill it.</p>
  </div>
{:else}
  <div class="list">
    {#each jobs as job (job.id)}
      <div class="card job">
        <div class="job-main">
          <div class="job-title">{titles[job.libraryItemId] ?? `item ${job.libraryItemId}`}</div>
          <div class="job-sub muted">
             {job.kind} · {formatBytes(job.bytesDone)} / {formatBytes(job.bytesTotal)} · {eta(job)}{attemptLabel(job)}
          </div>
          {#if job.status === 'running' || (job.bytesTotal > 0 && job.bytesDone > 0)}
            <div class="progress-track"><div class="progress-fill" style="width: {pct(job)}%"></div></div>
          {/if}
          {#if job.status === 'error' && job.error}
            <div class="job-err">{job.error}</div>
          {/if}
        </div>
        <div class="job-side">
          <span class="pill {statusClass(job.status)}">{job.status}</span>
          <div class="actions">
            {#if job.status === 'error'}
              <button class="btn-ghost sm" onclick={() => act(job.id, 'retry')}>Retry</button>
            {/if}
            {#if job.status === 'pending' || job.status === 'error'}
              <button class="btn-ghost sm" onclick={() => act(job.id, 'pause')}>Pause</button>
            {/if}
            {#if job.status === 'paused'}
              <button class="btn-ghost sm" onclick={() => act(job.id, 'resume')}>Resume</button>
            {/if}
            {#if job.status !== 'running'}
              <button class="btn-ghost sm" onclick={() => move(job.id, -1)}>↑</button>
              <button class="btn-ghost sm" onclick={() => move(job.id, 1)}>↓</button>
              <button class="btn-danger-ghost sm" onclick={() => act(job.id, job.status === 'pending' ? 'skip' : 'cancel')}>
                {job.status === 'pending' ? 'Skip' : 'Remove'}
              </button>
            {:else}
              <button class="btn-danger-ghost sm" onclick={() => act(job.id, 'cancel')}>Cancel</button>
            {/if}
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}

<style>
  .title-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 16px;
  }
  .notice {
    padding: 10px 14px;
    border-radius: 10px;
    background: var(--accent-soft);
    color: var(--accent);
    margin: 0 0 12px;
  }
  .notice.err {
    background: var(--danger-soft);
    color: var(--danger);
  }
  .empty {
    padding: 28px;
    text-align: center;
    color: var(--fg-muted);
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .job {
    display: flex;
    gap: 16px;
    padding: 14px 18px;
    align-items: flex-start;
  }
  .job-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .job-title {
    font-weight: 700;
  }
  .job-sub {
    font-size: 12px;
  }
  .job-err {
    color: var(--danger);
    font-size: 12px;
    word-break: break-word;
  }
  .job-side {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 8px;
  }
  .actions {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .sm {
    padding: 5px 10px;
    font-size: 12px;
  }
  .pill-danger {
    background: var(--danger-soft);
    color: var(--danger);
  }
</style>
