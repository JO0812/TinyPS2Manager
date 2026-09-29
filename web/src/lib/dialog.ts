// Native folder picker via the Wails binding bridge (window.go.main.App).
//
// The same web build runs in two hosts: the Wails desktop window (where the
// Go binding is registered) and plain browsers via `oplbm serve` (where
// window.go does not exist). browseFolder resolves null when no picker is
// available or the user cancels, so callers keep the typed path field.
declare global {
  interface Window {
    go?: {
      main?: {
        App?: {
          SelectFolder(title: string): Promise<string>;
        };
      };
    };
  }
}

export async function browseFolder(title: string): Promise<string | null> {
  try {
    const pick = window.go?.main?.App?.SelectFolder;
    if (typeof pick !== 'function') return null;
    const path = await pick.call(window.go?.main?.App, title);
    return path || null;
  } catch {
    return null;
  }
}
