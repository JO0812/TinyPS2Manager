// Dark-only app: there is no light mode. applyTheme pins <html> to dark
// unconditionally; initTheme is kept so the boot sequence doesn't change.
export function applyTheme(): void {
  document.documentElement.dataset.theme = 'dark';
}

export async function initTheme(): Promise<void> {
  applyTheme();
}
