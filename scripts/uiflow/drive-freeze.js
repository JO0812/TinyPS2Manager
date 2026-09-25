// Repro: Drive Options "Filesystem override" dropdown freezes the UI.
// Env: BASE=http://127.0.0.1:PORT CHROMIUM_BIN (default /usr/bin/chromium).
// Asserts the main thread stays alive across a dropdown change, the PATCH
// lands, and preflight settles (no request storm, no page errors).
const puppeteer = require('puppeteer-core');

const BASE = process.env.BASE;
const CHROMIUM = process.env.CHROMIUM_BIN || '/usr/bin/chromium';

const results = [];
function check(name, ok, extra = '') {
  results.push(`${ok ? 'PASS' : 'FAIL'} ${name} ${extra}`);
  if (!ok) process.exitCode = 1;
}

(async () => {
  const browser = await puppeteer.launch({
    executablePath: CHROMIUM,
    headless: 'new',
    args: ['--no-sandbox', '--disable-gpu'],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1600, height: 900 });
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));

  // Count API traffic to detect request storms.
  await page.evaluateOnNewDocument(() => {
    window.__fetchLog = [];
    const origFetch = window.fetch;
    window.fetch = (...args) => {
      try {
        const url = typeof args[0] === 'string' ? args[0] : args[0].url;
        window.__fetchLog.push(`${args[1] && args[1].method ? args[1].method : 'GET'} ${url}`);
      } catch {}
      return origFetch(...args);
    };
    window.__hb = 0;
    const tick = () => {
      window.__hb++;
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });

  await page.goto(BASE + '/', { waitUntil: 'networkidle0', timeout: 30000 });
  await new Promise((r) => setTimeout(r, 1000));

  // Go to Drive Options.
  await page.evaluate(() => {
    [...document.querySelectorAll('aside nav button')]
      .find((x) => (x.title || '').includes('Drive'))
      .click();
  });
  await new Promise((r) => setTimeout(r, 1500)); // let initial preflight settle

  const hb0 = await page.evaluate(() => window.__hb);
  await new Promise((r) => setTimeout(r, 500));
  const hb1 = await page.evaluate(() => window.__hb);
  check('heartbeat-alive-before', hb1 > hb0, `hb ${hb0}->${hb1}`);

  // Baseline: preflight section rendered?
  const pre0 = await page.evaluate(() => document.body.textContent.includes('Pre-flight checks'));
  check('preflight-section-present', pre0);

  // Clear fetch log, then change the dropdown like a user would.
  await page.evaluate(() => {
    window.__fetchLog = [];
  });
  const selHandle = await page.$('.detail select');
  check('override-select-present', !!selHandle);
  if (selHandle) {
    await selHandle.select('fat32');
    // waitForFunction polls over CDP; it times out if the main thread is frozen
    try {
      await page.waitForFunction(
        () => {
          const sel = document.querySelector('.detail select');
          return sel && sel.value === 'fat32';
        },
        { timeout: 5000 },
      );
      check('select-keeps-value', true);
    } catch {
      check('select-keeps-value', false, 'select never settled on fat32');
    }
  }

  // Give PATCH + refresh + preflight time to complete.
  await new Promise((r) => setTimeout(r, 3000));

  // Main thread still alive?
  let hb2 = -1;
  let hb3 = -1;
  try {
    hb2 = await page.evaluate(() => window.__hb, { timeout: 5000 });
    await new Promise((r) => setTimeout(r, 500));
    hb3 = await page.evaluate(() => window.__hb, { timeout: 5000 });
  } catch (e) {
    check('heartbeat-alive-after', false, `evaluate failed: ${e.message}`);
  }
  if (hb2 >= 0) check('heartbeat-alive-after', hb3 > hb2, `hb ${hb2}->${hb3}`);

  // PATCH landed server-side?
  const dests = await page.evaluate(() => fetch('/api/destinations').then((r) => r.json()));
  const ov = dests[0] && dests[0].fsOverride;
  check('patch-persisted', ov === 'fat32', `fsOverride=${ov}`);

  // Preflight storm check: one dropdown change should not fan out.
  const log = await page.evaluate(() => window.__fetchLog);
  const preflights = log.filter((l) => l.includes('/preflight')).length;
  const patches = log.filter((l) => l.startsWith('PATCH')).length;
  const lists = log.filter((l) => l === 'GET /api/destinations').length;
  check('no-preflight-storm', preflights <= 3, `preflight=${preflights} patch=${patches} list=${lists}`);
  check('single-patch', patches === 1, `patch=${patches}`);

  // Preflight rendered (pass or fail rows, not stuck on Checking…)?
  const rendered = await page.evaluate(() => {
    const items = [...document.querySelectorAll('.preflight .check')];
    return items.length;
  });
  check('preflight-rendered', rendered > 0, `rows=${rendered}`);

  check('no-page-errors', errors.length === 0, errors.join('; '));
  await browser.close();
  console.log(results.join('\n'));
})().catch((e) => {
  console.error('FLOW-ERROR', e.message);
  process.exit(1);
});
