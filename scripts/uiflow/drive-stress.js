// Stress: rapid Filesystem-override changes must not freeze, lose the
// choice, or leave stale preflight on screen.
// Env: BASE, CHROMIUM_BIN (default /usr/bin/chromium).
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
    window.__fetchLog = [];
    const origFetch = window.fetch;
    window.fetch = (...args) => {
      try {
        const url = typeof args[0] === 'string' ? args[0] : args[0].url;
        window.__fetchLog.push(`${args[1] && args[1].method ? args[1].method : 'GET'} ${url}`);
      } catch {}
      return origFetch(...args);
    };
    window.__hb = [];
    const tick = () => {
      window.__hb.push(Date.now());
      if (window.__hb.length > 2000) window.__hb.splice(0, 1000);
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });

  await page.goto(BASE + '/', { waitUntil: 'networkidle0', timeout: 30000 });
  await new Promise((r) => setTimeout(r, 1000));
  await page.evaluate(() => {
    [...document.querySelectorAll('aside nav button')]
      .find((x) => (x.title || '').includes('Drive'))
      .click();
  });
  await new Promise((r) => setTimeout(r, 1500));

  const worstStall = async () => {
    return page.evaluate(() => {
      const hb = window.__hb;
      let worst = 0;
      for (let i = 1; i < hb.length; i++) worst = Math.max(worst, hb[i] - hb[i - 1]);
      return worst;
    });
  };
  check('heartbeat-ok-before', (await worstStall()) < 1500, `worst=${await worstStall()}ms`);

  // Slow PATCH responses down so in-flight states are observable: the
  // choice must stay on screen (pending value) instead of snapping back.
  await page.evaluate(() => {
    const origFetch = window.fetch;
    window.fetch = (...args) => {
      const method = args[1] && args[1].method ? args[1].method : 'GET';
      if (method === 'PATCH') {
        return new Promise((resolve) => setTimeout(() => resolve(origFetch(...args)), 400));
      }
      return origFetch(...args);
    };
  });

  // Mid-save: dispatch a change, then read the select BEFORE the delayed
  // PATCH can resolve — pending display must already show the new value.
  const sel = await page.$('.detail select');
  check('override-select-present', !!sel);
  await page.evaluate(() => {
    const s = document.querySelector('.detail select');
    s.value = 'exfat';
    s.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await new Promise((r) => setTimeout(r, 150));
  const midSave = await page.evaluate(() => document.querySelector('.detail select').value);
  check('choice-sticks-mid-save', midSave === 'exfat', `select=${JSON.stringify(midSave)}`);
  await new Promise((r) => setTimeout(r, 1500)); // let delayed PATCH + refresh land

  // Rapid-fire two more changes with almost no gap between them.
  for (const v of ['fat32', '']) {
    await sel.select(v);
    await new Promise((r) => setTimeout(r, 120));
  }
  // Let everything settle.
  await new Promise((r) => setTimeout(r, 4000));

  const stall = await worstStall();
  check('no-main-thread-stall', stall < 1500, `worst rAF gap=${stall}ms`);

  // Final choice must stick (last write wins, value "").
  const finalVal = await page.evaluate(() => document.querySelector('.detail select').value);
  check('last-choice-sticks', finalVal === '', `select=${JSON.stringify(finalVal)}`);
  const serverOv = await page.evaluate(() =>
    fetch('/api/destinations').then((r) => r.json()).then((d) => d[0].fsOverride),
  );
  check('server-matches-last', serverOv === '', `fsOverride=${JSON.stringify(serverOv)}`);

  // Preflight must reflect the FINAL state, not a stale intermediate one.
  const fsRow = await page.evaluate(() => {
    const rows = [...document.querySelectorAll('.preflight .check')];
    const fs = rows.find((li) => li.textContent.includes('filesystem'));
    return fs ? fs.textContent : null;
  });
  // With override "" and detected overlay (unknown), expect the "unknown" warning text.
  check(
    'preflight-matches-final',
    !!fsRow && fsRow.includes('unknown'),
    `filesystem row=${JSON.stringify(fsRow && fsRow.slice(0, 120))}`,
  );

  // BDM prefix via the real input (same save path as the dropdown).
  await page.evaluate(() => {
    const input = document.querySelector('.detail input.field');
    const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
    setter.call(input, 'OPL');
    input.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await new Promise((r) => setTimeout(r, 2500));
  const prefixVal = await page.evaluate(() => document.querySelector('.detail input.field').value);
  const serverPrefix = await page.evaluate(() =>
    fetch('/api/destinations').then((r) => r.json()).then((d) => d[0].bdmPrefix),
  );
  check('prefix-roundtrip', prefixVal === 'OPL' && serverPrefix === 'OPL', `ui=${JSON.stringify(prefixVal)} server=${JSON.stringify(serverPrefix)}`);

  const log = await page.evaluate(() => window.__fetchLog);
  const patches = log.filter((l) => l.startsWith('PATCH')).length;
  const preflights = log.filter((l) => l.includes('/preflight')).length;
  check('four-patches', patches === 4, `patch=${patches} (3 fs + 1 prefix)`);
  console.log(`INFO requests patch=${patches} preflight=${preflights}`);

  check('no-page-errors', errors.length === 0, errors.join('; '));
  await browser.close();
  console.log(results.join('\n'));
})().catch((e) => {
  console.error('FLOW-ERROR', e.message);
  process.exit(1);
});
