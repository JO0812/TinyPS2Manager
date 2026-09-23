// Design monitor: screenshots every view for visual review.
// Env: BASE, OUT (default ./shots), CHROMIUM_BIN, DARK=1|0 (default 1).
const puppeteer = require('puppeteer-core');

const BASE = process.env.BASE || 'http://127.0.0.1:41337';
const OUT = process.env.OUT || './shots';
const CHROMIUM = process.env.CHROMIUM_BIN || '/usr/bin/chromium';
const DARK = process.env.DARK !== '0';

(async () => {
  const browser = await puppeteer.launch({
    executablePath: CHROMIUM,
    headless: 'new',
    args: ['--no-sandbox', '--disable-gpu', '--force-device-scale-factor=1'],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1600, height: 900 });
  if (DARK) await page.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: 'dark' }]);
  page.on('console', (m) => {
    if (m.type() === 'error') console.log('CONSOLE:', m.text());
  });
  page.on('pageerror', (e) => console.log('PAGEERROR:', e.message));
  await page.goto(BASE + '/', { waitUntil: 'networkidle0', timeout: 30000 });
  await new Promise((r) => setTimeout(r, 1200));
  await page.screenshot({ path: `${OUT}/library.png` });

  for (const [label, file] of [
    ['activity', 'activity.png'],
    ['drive', 'drive.png'],
    ['settings', 'settings.png'],
  ]) {
    await page.evaluate((t) => {
      const btns = [...document.querySelectorAll('aside nav button, aside .sidebar-foot button')];
      const b = btns.find((x) => (x.getAttribute('title') || '').toLowerCase().includes(t));
      if (b) b.click();
    }, label);
    await new Promise((r) => setTimeout(r, 800));
    await page.screenshot({ path: `${OUT}/${file}` });
  }
  await browser.close();
  console.log('shots in', OUT);
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
