// Behavioral UI tests: drive the real app end to end and assert outcomes.
// Env: BASE=http://127.0.0.1:PORT FIX=<fixtures> DEV=<device>
//      CHROMIUM_BIN (default /usr/bin/chromium), DARK=1|0 (default 1).
// Exit nonzero on any failure. Needs `npm ci` in this dir first.
const puppeteer = require('puppeteer-core');

const BASE = process.env.BASE;
const FIX = process.env.FIX;
const DEV = process.env.DEV;
const CHROMIUM = process.env.CHROMIUM_BIN || '/usr/bin/chromium';
const DARK = process.env.DARK !== '0';

const results = [];
function check(name, ok, extra = '') {
  results.push(`${ok ? 'PASS' : 'FAIL'} ${name} ${extra}`);
  if (!ok) process.exitCode = 1;
}

async function setInput(page, selector, value) {
  await page.evaluate(
    (sel, val) => {
      const input = document.querySelector(sel);
      if (!input) throw new Error('missing ' + sel);
      const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
      setter.call(input, val);
      input.dispatchEvent(new Event('input', { bubbles: true }));
    },
    selector,
    value,
  );
}

async function clickText(page, scope, text) {
  await page.evaluate(
    (sel, t) => {
      const root = sel ? document.querySelector(sel) : document;
      const btn = [...root.querySelectorAll('button')].find((b) => (b.textContent || '').includes(t));
      if (!btn) throw new Error('missing button ' + t);
      btn.click();
    },
    scope,
    text,
  );
}

(async () => {
  const browser = await puppeteer.launch({
    executablePath: CHROMIUM,
    headless: 'new',
    args: ['--no-sandbox', '--disable-gpu'],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1600, height: 900 });
  if (DARK) await page.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: 'dark' }]);
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(BASE + '/', { waitUntil: 'networkidle0', timeout: 30000 });
  await new Promise((r) => setTimeout(r, 1000));

  // 1. Import via the UI form (5 items group into 4 cards: 2 PS2
  // singles + 1 PS1 pair + 1 PS1 single).
  await setInput(page, '.import-row input', FIX);
  await clickText(page, '.import-row', 'Scan');
  await page.waitForFunction(() => [...document.querySelectorAll('.game-card')].length >= 4, {
    timeout: 15000,
  });
  const itemCount = await page.evaluate(() => fetch('/api/library').then((r) => r.json()).then((d) => d.length));
  check('import-via-ui', itemCount === 5, `items=${itemCount}`);

  // 2. Destination via Drive Options UI.
  await page.evaluate(() => {
    [...document.querySelectorAll('aside nav button')].find((x) => x.title.includes('Drive')).click();
  });
  await new Promise((r) => setTimeout(r, 500));
  await setInput(page, '.drives input', DEV);
  await clickText(page, '.drives', 'Add');
  await page.waitForFunction(() => document.body.textContent.includes('Destination added'), {
    timeout: 10000,
  });
  check('add-destination-via-ui', true);

  // 2b. Filesystem-override dropdown via the real UI select (regression:
  // the Drive menu used to freeze/lose the choice here).
  await page.evaluate(() => {
    window.__hb = 0;
    const tick = () => {
      window.__hb++;
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  const sel = await page.$('.detail select');
  check('override-select-present', !!sel);
  await sel.select('fat32');
  await page.waitForFunction(() => document.querySelector('.detail select').value === 'fat32', {
    timeout: 10000,
  });
  await page.waitForFunction(() => [...document.querySelectorAll('.preflight .check')].length > 0, {
    timeout: 15000,
  });
  const hbA = await page.evaluate(() => window.__hb);
  await new Promise((r) => setTimeout(r, 500));
  const hbB = await page.evaluate(() => window.__hb);
  check('drive-dropdown-responsive', hbB > hbA, `hb ${hbA}->${hbB}`);
  const ov = await page.evaluate(() => {
    const id = Number(localStorage.getItem('oplbm.destId') || '0');
    return fetch('/api/destinations')
      .then((r) => r.json())
      .then((d) => (d.find((x) => x.id === id) || {}).fsOverride);
  });
  check('drive-dropdown-persisted', ov === 'fat32', `fsOverride=${ov}`);

  // 3. Enqueue first card via its … menu.
  await page.evaluate(() => {
    [...document.querySelectorAll('aside nav button')].find((x) => x.title.includes('Library')).click();
  });
  await new Promise((r) => setTimeout(r, 800));
  await page.evaluate(async () => {
    const dests = await (await fetch('/api/destinations')).json();
    await fetch(`/api/destinations/${dests[0].id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ filesystemOverride: 'exfat' }),
    });
    localStorage.setItem('oplbm.destId', String(dests[0].id));
  });
  await page.evaluate(() => {
    document.querySelector('.game-card .dots').click();
  });
  await new Promise((r) => setTimeout(r, 300));
  await page.evaluate(() => {
    [...document.querySelectorAll('.game-card .menu button')]
      .find((b) => b.textContent.includes('Add to queue'))
      .click();
  });
  await page.waitForFunction(
    async () => {
      const jobs = await (await fetch('/api/queue')).json();
      return jobs.length >= 1 && jobs.every((j) => j.status === 'done');
    },
    { timeout: 30000 },
  );
  check('enqueue-via-card-menu', true);

  // 4. Activity pause-all/resume-all buttons.
  await page.evaluate(() => {
    [...document.querySelectorAll('aside nav button')].find((x) => x.title.includes('Activity')).click();
  });
  await new Promise((r) => setTimeout(r, 800));
  const paused = await page.evaluate(async () => {
    [...document.querySelectorAll('main button')].find((b) => b.textContent.includes('Pause all')).click();
    await new Promise((r) => setTimeout(r, 300));
    return document.body.textContent.includes('Resume all');
  });
  check('pause-all-toggle', paused);
  await page.evaluate(() => {
    [...document.querySelectorAll('main button')].find((b) => b.textContent.includes('Resume all')).click();
  });

  // 5. Dark-only: no theme toggle exists and theme stays dark.
  await page.evaluate(() => {
    [...document.querySelectorAll('aside .sidebar-foot button')][0].click();
  });
  await new Promise((r) => setTimeout(r, 800));
  const themeBtns = await page.evaluate(() => document.querySelectorAll('.themes button').length);
  check('no-theme-toggle', themeBtns === 0, `theme buttons=${themeBtns}`);
  const theme = await page.evaluate(() => document.documentElement.dataset.theme);
  check('dark-only', theme === 'dark', `theme=${theme}`);

  check('no-page-errors', errors.length === 0, errors.join('; '));
  await browser.close();
  console.log(results.join('\n'));
})().catch((e) => {
  console.error('FLOW-ERROR', e.message);
  process.exit(1);
});
