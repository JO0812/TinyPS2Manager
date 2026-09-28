// Single active-drive store (TinyWiiBackupManager-style mount-point UX).
//
// The backend still tracks many destinations; the UI shows exactly one
// active drive everywhere (header bar, Games gate, Toolbox). The choice
// persists in localStorage under the historic oplbm.destPath key so game
// cards, the tour, and older sessions keep working.
import { derived, get, writable } from 'svelte/store';
import { api, formatBytes, type Destination, type PreflightResult, type Volume } from './api';

const KEY = 'oplbm.destPath';

function stored(): string {
  try {
    return localStorage.getItem(KEY) || '';
  } catch {
    return '';
  }
}

/** Synchronous read for event handlers (game cards, tour). */
export function getActivePath(): string {
  return get(activePath) || stored();
}

export function setActivePath(p: string) {
  try {
    localStorage.setItem(KEY, p);
  } catch {
    /* private mode: store still works for the session */
  }
  activePath.set(p);
  void loadPreflight(p);
}

export const activePath = writable<string>(stored());
export const destinations = writable<Destination[]>([]);
export const volumes = writable<Volume[]>([]);
export const drivesError = writable('');

export const activeDest = derived([destinations, activePath], ([$dests, $path]) =>
  $dests.find((d) => d.path === $path),
);

export const driveLabel = derived(activeDest, ($d) => {
  if (!$d) return '';
  const leaf = $d.path.split('/').filter(Boolean).pop() ?? $d.path;
  return `${leaf} · ${( $d.fsOverride || $d.filesystem).toUpperCase()}`;
});

export const driveSpace = derived(activeDest, ($d) => {
  if (!$d || $d.totalBytes <= 0) return '';
  const used = $d.totalBytes - Math.max(0, $d.freeBytes);
  return `${formatBytes($d.freeBytes)} free of ${formatBytes($d.totalBytes)}`;
});

// Preflight for the active drive, sequence-guarded so rapid drive
// switches can't let a stale response clobber newer state.
export const preflight = writable<PreflightResult | null>(null);
export const preflightBusy = writable(false);
export const preflightError = writable('');

let preflightSeq = 0;

export async function loadPreflight(path: string) {
  if (!path) {
    preflight.set(null);
    return;
  }
  const seq = ++preflightSeq;
  preflightBusy.set(true);
  preflightError.set('');
  try {
    const res = await api.preflight(path);
    if (seq !== preflightSeq) return;
    preflight.set(res);
  } catch (e) {
    if (seq !== preflightSeq) return;
    preflightError.set(e instanceof Error ? e.message : String(e));
    preflight.set(null);
  } finally {
    if (seq === preflightSeq) preflightBusy.set(false);
  }
}

/** Refresh destinations + volumes; heal a stale stored path; preflight the winner. */
export async function refreshDrives(): Promise<Destination[]> {
  try {
    const [dests, vols] = await Promise.all([api.destinations(), api.volumes()]);
    destinations.set(dests);
    volumes.set(vols);
    drivesError.set('');
    let cur = get(activePath) || stored();
    if (!dests.some((d) => d.path === cur)) {
      cur = dests[0]?.path ?? '';
      try {
        localStorage.setItem(KEY, cur);
      } catch {
        /* ignore */
      }
      activePath.set(cur);
    }
    await loadPreflight(cur);
    return dests;
  } catch (e) {
    drivesError.set(e instanceof Error ? e.message : String(e));
    return [];
  }
}
