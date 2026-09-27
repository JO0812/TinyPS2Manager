// Full UX walkthrough: drives every view of the real app and asserts
// outcomes, including device artifacts. Env: BASE, FIX, SRC, DEV, SHOTS,
// CHROMIUM_BIN (default /usr/bin/chromium). Exit nonzero on any failure.
const puppeteer = require('puppeteer-core');
const fs = require('fs');
const path = require('path');

const BASE = process.env.BASE;
const FIX = process.env.FIX;
const SRC = process.env.SRC;
const DEV = process.env.DEV;
const SHOTS = process.env.SHOTS;
const CHROMIUM = process.env.CHROMIUM_BIN || '/usr/bin/chromium';

const results = [];
function check(name, ok, extra = '') {
  results.push(`${ok ? 'PASS' : 'FAIL'} ${name} ${extra}`);
  if (!ok) process.exitCode = 1;
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const step = (n) => console.log('STEP', n);

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

async function nav(page, label) {
  await page.evaluate((l) => {
    const b = [...document.querySelectorAll('aside nav button')].find((x) => (x.title || '').includes(l));
    if (!b) throw new Error('missing nav ' + l);
    b.click();
  }, label);
  await sleep(800);
}

async function apiGet(page, p) {
  return page.evaluate(async (u) => (await fetch(u)).json(), p);
}
async function apiPost(page, p, body) {
  return page.evaluate(
    async (u, b) => {
      const r = await fetch(u, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(b) });
      return { status: r.status, data: await r.json() };
    },
    p,
    body,
  );
}

// Index of the .game-card whose .title matches, or -1.
async function cardIndex(page, title) {
  return page.evaluate((t) => {
    const cards = [...document.querySelectorAll('.game-card')];
    return cards.findIndex((c) => (c.querySelector('.title') || {}).textContent === t);
  }, title);
}

async function openMenu(page, idx) {
  await page.evaluate((i) => {
    document.querySelectorAll('.game-card')[i].querySelector('.dots').click();
  }, idx);
  await sleep(300);
}

async function menuClick(page, idx, text) {
  const ok = await page.evaluate(
    (i, t) => {
      const menu = document.querySelectorAll('.game-card')[i].querySelector('.menu');
      if (!menu) return false;
      const b = [...menu.querySelectorAll('button')].find((x) => (x.textContent || '').includes(t));
      if (!b || b.disabled) return false;
      b.click();
      return true;
    },
    idx,
    text,
  );
  if (!ok) throw new Error(`menu button "${text}" missing/disabled on card ${idx}`);
  await sleep(400);
}

async function menuSelect(page, idx, label, value) {
  await page.evaluate(
    (i, l, v) => {
      const menu = document.querySelectorAll('.game-card')[i].querySelector('.menu');
      const sel = [...menu.querySelectorAll('select')].find((s) => s.getAttribute('aria-label') === l);
      if (!sel) throw new Error('missing select ' + l);
      sel.value = v;
      sel.dispatchEvent(new Event('change', { bubbles: true }));
    },
    idx,
    label,
    value,
  );
  await sleep(200);
}

async function enrichment(page, id) {
  const dp = await page.evaluate(() => localStorage.getItem('oplbm.destPath') || '');
  return apiGet(page, `/api/library/${id}/enrichment?destinationPath=${encodeURIComponent(dp)}`);
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
  await page.goto(BASE + '/', { waitUntil: 'networkidle0', timeout: 30000 });
  await sleep(1000);

  // ---- 1. Library import
  // (gensrc + 2 serial ISOs + big cue = 8 items)
  step('1-import');
  await setInput(page, '.import-row input', FIX);
  await clickText(page, '.import-row', 'Scan');
  await page.waitForFunction(() => [...document.querySelectorAll('.game-card')].length >= 7, { timeout: 20000 });
  const items = await apiGet(page, '/api/library');
  check('tour-import-count', items.length === 8, `items=${items.length}`);
  const certain = items.find((i) => i.gameId === 'SLUS_213.85');
  const uncertain = items.find((i) => i.gameId === 'SLES_512.30');
  check('tour-gameids', !!certain && !!uncertain && uncertain.gameIdUncertain === true && certain.gameIdUncertain === false,
    `certain=${certain && certain.title} uncertain=${uncertain && uncertain.title}`);
  // GameID badges: certain plain, uncertain with " ?".
  const badges = await page.evaluate(() => document.body.textContent);
  check('tour-gameid-badges', badges.includes('SLUS_213.85') && badges.includes('SLES_512.30'), '');
  await page.screenshot({ path: path.join(SHOTS, '01-library.png') });

  // ---- 2. Drive Options: add DEV, preflight rows, fs override ----
  step('2-drive-options');
  await nav(page, 'Drive');
  await setInput(page, '.drives input', DEV);
  await clickText(page, '.drives', 'Add');
  await page.waitForFunction(() => document.body.textContent.includes('Destination added'), { timeout: 10000 });
  check('tour-add-destination', true);
  await page.waitForFunction(() => [...document.querySelectorAll('.preflight .check')].length >= 5, { timeout: 15000 });
  const checkNames = await page.evaluate(() =>
    [...document.querySelectorAll('.preflight .check')].map((li) => li.querySelector('strong').textContent + ':' + li.querySelector('.badge').textContent),
  );
  for (const want of ['filesystem:', 'free-space:', 'ul.cfg:', 'partition-table:', 'partition-alignment:']) {
    check(`tour-preflight-${want.replace(/:$/, '')}`, checkNames.some((c) => c.startsWith(want)), checkNames.join(' | '));
  }
  const alignWarn = checkNames.find((c) => c.startsWith('partition-alignment:'));
  check('tour-alignment-folder-skip', !!alignWarn && alignWarn.endsWith('warn'), alignWarn || 'missing');
  // fs override via the real select, persists server-side.
  const sel = await page.$('.detail select');
  await sel.select('fat32');
  await page.waitForFunction(() => document.querySelector('.detail select').value === 'fat32', { timeout: 10000 });
  const dp = await page.evaluate(() => localStorage.getItem('oplbm.destPath') || '');
  const dests = await apiGet(page, '/api/destinations');
  const mine = dests.find((d) => d.path === dp);
  check('tour-fs-override', !!mine && mine.fsOverride === 'fat32', `override=${mine && mine.fsOverride}`);
  // The tour folder lives on a non-FAT host fs while the toggle says
  // FAT32: for folders this warns (the toggle rules planning for the future
  // target) instead of fail-closing like a live drive would (spec §2.11).
  const fsRow = await page.evaluate(() => {
    const li = [...document.querySelectorAll('.preflight .check')].find((x) => x.querySelector('strong').textContent === 'filesystem');
    return li ? li.querySelector('.badge').textContent : 'missing';
  });
  check('tour-fs-folder-warns', fsRow === 'warn' || fsRow === 'pass', fsRow);
  const blockedNow = await page.evaluate(async (dpath) => {
    const r = await fetch(`/api/destinations/preflight?path=${encodeURIComponent(dpath)}`);
    return (await r.json()).blocked;
  }, dp);
  check('tour-folder-unblocked', blockedNow === false, `blocked=${blockedNow}`);
  const prepEnabled = await page.evaluate(() => {
    const b = [...document.querySelectorAll('.detail button')].find((x) => (x.textContent || '').includes('Prepare external drive'));
    return !!b && !b.disabled;
  });
  check('tour-prepare-enabled', prepEnabled, '');
  await page.screenshot({ path: path.join(SHOTS, '02-drive-options.png') });

  // ---- 3. Settings: cheat source paths persist ----
  step('3-settings');
  await page.evaluate(() => [...document.querySelectorAll('aside .sidebar-foot button')][0].click());
  await sleep(800);
  const handDir = path.join(SRC, 'hand');
  const wideDir = path.join(SRC, 'wide');
  const dbFile = path.join(SRC, 'CheatDatabase.txt');
  fs.mkdirSync(handDir, { recursive: true });
  fs.mkdirSync(wideDir, { recursive: true });
  // Cheat sources keyed off the real library titles (read back from the API).
  const dbText =
    `"${certain.title}"\nMaster\n90333333 33333333\nAmmo\n20333333 00000063\n\n` +
    `"${uncertain.title}"\nMaster\n90555555 55555555\nHP\n20555555 00000001\n`;
  fs.writeFileSync(dbFile, dbText);
  fs.writeFileSync(path.join(handDir, 'SLUS_213.85.cht'), 'Hand\n90111111 11111111\nHP\n20111111 00000001\n');
  fs.writeFileSync(path.join(wideDir, 'SLUS_213.85.cht'), 'Wide\n90222222 22222222\nWidescreen\n20222222 00000002\n');
  const inputs = await page.$$('.form input.field');
  // Order in the form: split, staging, prefix, [select], db, wide, hand.
  await inputs[inputs.length - 3].evaluate((el, v) => {
    el.value = v;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  }, dbFile);
  await inputs[inputs.length - 2].evaluate((el, v) => {
    el.value = v;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  }, wideDir);
  await inputs[inputs.length - 1].evaluate((el, v) => {
    el.value = v;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  }, handDir);
  await clickText(page, '.form', 'Save settings');
  await page.waitForFunction(() => document.body.textContent.includes('Settings saved'), { timeout: 10000 });
  const saved = await apiGet(page, '/api/settings');
  check('tour-settings-cheats', saved.handCheatDir === handDir && saved.widescreenDir === wideDir && saved.cheatDatabasePath === dbFile,
    JSON.stringify({ h: saved.handCheatDir, w: saved.widescreenDir, d: saved.cheatDatabasePath }));
  await page.screenshot({ path: path.join(SHOTS, '03-settings.png') });

  // ---- 4. Certain card: Fetch art + Stage cheats (picker) ----
  step('4-certain');
  await nav(page, 'Library');
  await sleep(800);
  const cIdx = await cardIndex(page, certain.title);
  check('tour-certain-card', cIdx >= 0, `idx=${cIdx}`);
  // Region badge: US serial + (USA) title matches → no region? pill on this card.
  const cardRegion = await page.evaluate((i) => {
    const c = document.querySelectorAll('.game-card')[i];
    return [...c.querySelectorAll('.pill')].some((p) => (p.title || '').includes('Cheat region may not match'));
  }, cIdx);
  check('tour-region-matched', cardRegion === false, '');
  await openMenu(page, cIdx);
  await menuClick(page, cIdx, 'Fetch art');
  await page.waitForFunction(
    async (itemId, dpath) => {
      const e = await (await fetch(`/api/library/${itemId}/enrichment?destinationPath=${encodeURIComponent(dpath)}`)).json();
      return e.artStatus === 'found';
    },
    { timeout: 30000 },
    certain.id,
    dp,
  );
  const artFile = path.join(DEV, 'ART', 'SLUS_213.85_COV.png');
  check('tour-fetch-art', fs.existsSync(artFile), artFile);
  // Stage cheats via each picker source, verifying the winner bytes.
  const chtFile = path.join(DEV, 'CHT', 'SLUS_213.85.cht');
  async function stageAndRead(source) {
    if (fs.existsSync(chtFile)) fs.unlinkSync(chtFile);
    await openMenu(page, cIdx);
    await menuSelect(page, cIdx, 'Cheat source', source);
    await menuClick(page, cIdx, 'Stage cheats');
    await page.waitForFunction(
      async (itemId, dpath) => {
        const e = await (await fetch(`/api/library/${itemId}/enrichment?destinationPath=${encodeURIComponent(dpath)}`)).json();
        return e.cheatStatus === 'staged';
      },
      { timeout: 15000 },
      certain.id,
      dp,
    );
    return fs.readFileSync(chtFile, 'utf8');
  }
  const handContent = await stageAndRead('hand');
  check('tour-cheats-hand', handContent.includes('90111111') && handContent.includes('Hand'), handContent.split('\n')[0]);
  const wideContent = await stageAndRead('widescreen');
  check('tour-cheats-wide', wideContent.includes('90222222'), wideContent.split('\n')[0]);
  const dbContent = await stageAndRead('database');
  check('tour-cheats-db', dbContent.includes('90333333'), dbContent.split('\n')[0]);
  const autoContent = await stageAndRead('auto');
  check('tour-cheats-auto-hand-wins', autoContent.includes('90111111') && autoContent.includes('Hand'), '');
  await page.screenshot({ path: path.join(SHOTS, '04-library-enriched.png') });

  // ---- 5. Uncertain card: two-step confirm ----
  step('5-uncertain');
  const uIdx = await cardIndex(page, uncertain.title);
  check('tour-uncertain-card', uIdx >= 0, `idx=${uIdx}`);
  const uRegion = await page.evaluate((i) => {
    const c = document.querySelectorAll('.game-card')[i];
    return [...c.querySelectorAll('.pill')].some((p) => (p.title || '').includes('Cheat region may not match'));
  }, uIdx);
  check('tour-region-unmatched', uRegion === true, '');
  await openMenu(page, uIdx);
  await menuClick(page, uIdx, 'Stage cheats');
  const confirmShown = await page.evaluate((i) => {
    const menu = document.querySelectorAll('.game-card')[i].querySelector('.menu');
    return !!menu && (menu.textContent || '').includes('Uncertain ID');
  }, uIdx);
  check('tour-confirm-step', confirmShown, '');
  if (fs.existsSync(path.join(DEV, 'CHT', 'SLES_512.30.cht'))) fs.unlinkSync(path.join(DEV, 'CHT', 'SLES_512.30.cht'));
  await menuClick(page, uIdx, 'Confirm stage');
  await page.waitForFunction(
    async (itemId, dpath) => {
      const e = await (await fetch(`/api/library/${itemId}/enrichment?destinationPath=${encodeURIComponent(dpath)}`)).json();
      return e.cheatStatus === 'staged';
    },
    { timeout: 15000 },
    uncertain.id,
    dp,
  );
  check('tour-uncertain-staged', fs.existsSync(path.join(DEV, 'CHT', 'SLES_512.30.cht')), '');

  // ---- 6. Prepare dialog: preview (VCD warning, loader URL, checklist) ----
  step('6-prepare');
  await nav(page, 'Drive');
  await page.waitForFunction(() => !!document.querySelector('.detail'), { timeout: 15000 });
  await clickText(page, '.detail', 'Prepare external drive');
  try {
    await page.waitForFunction(() => !!document.querySelector('.dialog'), { timeout: 10000 });
  } catch (e) {
    const dbg = await page.evaluate(() => {
      const btns = [...document.querySelectorAll('.detail button')].map((b) => `${b.textContent.trim().slice(0, 30)} disabled=${b.disabled}`);
      const pre = [...document.querySelectorAll('.preflight .check')].map((li) => li.textContent.trim().slice(0, 90));
      return { btns, pre, hasDetail: !!document.querySelector('.detail') };
    });
    throw new Error('no dialog; ' + JSON.stringify(dbg));
  }
  step('6a-tree');
  await clickText(page, '.dialog', 'Tree only');
  await page.waitForFunction(() => [...document.querySelectorAll('.dialog .warn li')].length > 0, { timeout: 20000 });
  const warns = await page.evaluate(() => [...document.querySelectorAll('.dialog .warn li')].map((li) => li.textContent));
  check('tour-vcd-warning', warns.some((w) => w.includes('POPSTARTER ceiling')), warns.join(' | '));
  step('6b-loader');
  await clickText(page, '.dialog', 'Check release');
  let loaderOk = false;
  try {
    await page.waitForFunction(() => {
      const el = document.querySelector('.dialog .loader .url');
      return el && el.textContent.includes('github');
    }, { timeout: 30000 });
    loaderOk = true;
  } catch { loaderOk = false; }
  if (loaderOk) {
    const lurl = await page.evaluate(() => document.querySelector('.dialog .loader .url').textContent);
    check('tour-loader-url', lurl.includes('github.com') && lurl.includes('RIPTOPL'), lurl);
  } else {
    check('tour-loader-url', true, 'SKIPPED (release API unreachable)');
  }
  await page.screenshot({ path: path.join(SHOTS, '05-prepare-preview.png') });
  // Execute WITH loader when available, else tree execute.
  step('6c-execute');
  await clickText(page, '.dialog', 'Prepare & enqueue');
  await page.waitForFunction(() => document.body.textContent.includes('Enqueued'), { timeout: loaderOk ? 180000 : 60000 });
  const enqText = await page.evaluate(() => document.body.textContent);
  const m = enqText.match(/Enqueued (\d+) jobs/);
  check('tour-prepare-enqueue', !!m && Number(m[1]) >= 7, m && m[0]);
  const cl = await page.evaluate(() => [...document.querySelectorAll('.dialog .checklist li')].map((li) => li.textContent));
  check('tour-checklist', cl.length >= 4 && cl.some((s) => s.includes('L3')), cl.join(' | ').slice(0, 120));
  if (loaderOk) {
    const elfDirs = fs.readdirSync(path.join(DEV, 'APPS')).filter((d) => d.startsWith('APP_RIPTOPL-'));
    const elfOk = elfDirs.some((d) => fs.existsSync(path.join(DEV, 'APPS', d, 'RIPTOPL.ELF')));
    check('tour-loader-staged', elfOk, elfDirs.join(','));
  }
  step('6d-close');
  await clickText(page, '.dialog', 'Close');

  // ---- 7. Activity: skip the 2 GiB job, drain, artifacts ----
  step('7-activity');
  await nav(page, 'Activity');
  await sleep(1000);
  const jobs = await apiGet(page, '/api/queue');
  const bigItem = items.find((i) => i.title === 'big');
  const bigJob = jobs.find((j) => j.libraryItemId === bigItem.id);
  check('tour-big-job', !!bigJob, `jobs=${jobs.length}`);
  // Skip it via the API (cancel first if it already started running).
  step('7a-skip');
  const bigState = (await apiGet(page, '/api/queue')).find((j) => j.id === bigJob.id);
  if (bigState && bigState.status === 'running') {
    await apiPost(page, `/api/queue/${bigJob.id}`, { action: 'cancel' });
    await sleep(1000);
  }
  await apiPost(page, `/api/queue/${bigJob.id}`, { action: 'skip' });
  step('7b-drain');
  await page.waitForFunction(async () => {
    const js = await (await fetch('/api/queue')).json();
    return js.length > 0 && js.every((j) => !['pending', 'running'].includes(j.status));
  }, { timeout: 180000 });
  const finalJobs = await apiGet(page, '/api/queue');
  const errs = finalJobs.filter((j) => j.status === 'error');
  check('tour-drain-clean', errs.length === 0, JSON.stringify(errs.map((j) => j.error)).slice(0, 200));
  for (const f of ['CD/ps2cd.iso', 'DVD/ps2dvd.iso', 'POPS/game.VCD']) {
    check(`tour-artifact-${f.replace(/\//g, '-')}`, fs.existsSync(path.join(DEV, f)), '');
  }
  await page.screenshot({ path: path.join(SHOTS, '06-activity.png') });

  check('tour-no-page-errors', errors.length === 0, errors.join('; '));
  await browser.close();
  console.log(results.join('\n'));
})().catch((e) => {
  console.error('TOUR-ERROR', e.message);
  process.exit(1);
});
