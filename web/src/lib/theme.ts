import { api, type Settings } from './api';

export type ThemeChoice = Settings['theme'];
export type ResolvedTheme = 'light' | 'dark';

/** Resolve system/light/dark to a concrete theme. */
export function resolveTheme(choice: ThemeChoice): ResolvedTheme {
  if (choice === 'light' || choice === 'dark') return choice;
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

/** Apply the theme to <html> (data-theme drives the CSS variables). */
export function applyTheme(choice: ThemeChoice): void {
  document.documentElement.dataset.theme = resolveTheme(choice);
}

/** Load server settings, apply the stored theme, and keep it live. */
export async function initTheme(): Promise<Settings> {
  applyTheme('dark'); // mockup default before settings arrive
  const settings = await api.settings();
  applyTheme(settings.theme);
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    api
      .settings()
      .then((s) => applyTheme(s.theme))
      .catch(() => {});
  });
  return settings;
}
