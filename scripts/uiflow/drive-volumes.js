// Volumes picker: detected drives appear in the Drive Options dropdown,
// picking one fills the form (kind=drive), Add tracks it, and it then
// shows as (added). Env: BASE, CHROMIUM_BIN (default /usr/bin/chromium).
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

  await page.evaluateOnNewDocument(() => {
    window.__hb = 0;
    const tick = () => {
      window.__hb++;
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });

  await page.goto(BASE + '/', { waitUntil: 'networkidle0', timeout: 30000 });
  await new Promise((r) => setTimeout(r, 1000));

  // API contract first.
  const vols = await page.evaluate(() => fetch('/api/destinations/volumes').then((r) => r.json()));
  check('volumes-array', Array.isArray(vols), `n=${vols.length}`);
  const bad = vols.filter((v) => !v.path || !('added' in v) || !('label' in v));
  check('volumes-shape', bad.length === 0, bad.length ? JSON.stringify(bad[0]) : `${vols.length} vols`);

  await page.evaluate(() => {
    [...document.querySelectorAll('aside nav button')]
      .find((x) => (x.title || '').includes('Drive'))
      .click();
  });
  await new Promise((r) => setTimeout(r, 1200));

  const picker = await page.$('.drives select[aria-label="Detected drives"]');
  if (vols.length === 0) {
    check('volumes-fallback', !picker, 'no vols on this machine; manual form only');
  } else {
    check('volumes-picker-present', !!picker, `n=${vols.length}`);
    // Every detected volume must appear as an option.
    const missing = await page.evaluate((paths) => {
      const opts = [...document.querySelectorAll('.drives select[aria-label="Detected drives"] option')].map(
        (o) => o.value,
      );
      return paths.filter((p) => !opts.includes(p));
    }, vols.map((v) => v.path));
    check('volumes-all-listed', missing.length === 0, missing.join(','));

    // Pick the first unadded volume through the real select.
    const target = vols.find((v) => !v.added);
    if (target) {
      await page.evaluate((path) => {
        const sel = document.querySelector('.drives select[aria-label="Detected drives"]');
        sel.value = path;
        sel.dispatchEvent(new Event('change', { bubbles: true }));
      }, target.path);
      await new Promise((r) => setTimeout(r, 400));
      const form = await page.evaluate(() => ({
        path: document.querySelector('.drives input[aria-label="New destination path"]').value,
        kind: document.querySelector('.drives select[aria-label="Kind"]').value,
      }));
      check('pick-fills-form', form.path === target.path && form.kind === 'drive', JSON.stringify(form));
      await page.evaluate(() => {
        [...document.querySelectorAll('.drives button')].find((b) => b.textContent === 'Add').click();
      });
      await page.waitForFunction(() => document.body.textContent.includes('Destination added'), {
        timeout: 10000,
      });
      check('pick-add-tracks', true);
      const addedFlag = await page.evaluate((path) =>
        fetch('/api/destinations/volumes')
          .then((r) => r.json())
          .then((vs) => (vs.find((v) => v.path === path) || {}).added),
      target.path);
      check('pick-marks-added', addedFlag === true, `added=${addedFlag}`);
    } else {
      check('pick-fills-form', true, 'all vols already added; skipped');
      check('pick-add-tracks', true, 'skipped');
      check('pick-marks-added', true, 'skipped');
    }
  }

  const hbA = await page.evaluate(() => window.__hb);
  await new Promise((r) => setTimeout(r, 500));
  const hbB = await page.evaluate(() => window.__hb);
  check('heartbeat-alive', hbB > hbA, `hb ${hbA}->${hbB}`);
  check('no-page-errors', errors.length === 0, errors.join('; '));
  await browser.close();
  console.log(results.join('\n'));
})().catch((e) => {
  console.error('FLOW-ERROR', e.message);
  process.exit(1);
});
