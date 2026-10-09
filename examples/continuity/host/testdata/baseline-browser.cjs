// Invoked only by the required-service Go test after an explicit operator grant.
// All private input arrives via stdin. Emit only finite counts; never diagnostics,
// DOM, cookies, form tokens, URLs, screenshots, recordings or storage state.
'use strict';
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const path = require('node:path');
const evidence = {ok: false, pages: 0, navigations: 0, layouts: 0, keyboard: 0, forms: 0, denied: 0, external: 0, mutations: 0};

(async () => {
 process.stdin.setEncoding('utf8');
 let raw = '';
 for await (const chunk of process.stdin) {
  raw += chunk;
  if (Buffer.byteLength(raw) > 16384) throw Error('input-bound');
 }
 const input = JSON.parse(raw);
 raw = '';
 const origin = new URL(input.origin);
 assert.equal(origin.protocol, 'https:');
 assert.ok(origin.hostname === '127.0.0.1' || origin.hostname === '[::1]');
 assert.equal(origin.pathname, '/');
 assert.ok(path.isAbsolute(input.playwright) && path.isAbsolute(input.chromium));
 assert.equal(typeof input.sessionCookie, 'string');
 assert.ok(input.sessionCookie.length > 0 && input.sessionCookie.length <= 256);
 for (const key of ['property', 'case', 'application', 'procedure', 'source']) {
  assert.match(input[key], /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
 }
 const {chromium} = require(input.playwright);
 let browser;
 let launching;
 let interrupted = false;
 let rejectDeadline;
 const deadline = new Promise((_, reject) => { rejectDeadline = reject; });
 const interrupt = () => { interrupted = true; rejectDeadline(Error('interrupted')); };
 process.on('SIGINT', interrupt);
 process.on('SIGTERM', interrupt);
 const timer = setTimeout(interrupt, 35000);
 try {
  await Promise.race([deadline, (async () => {
   launching = chromium.launch({
    executablePath: input.chromium, headless: true, timeout: 10000,
    args: ['--no-sandbox', '--disable-background-networking', '--disable-component-update', '--disable-sync', '--no-first-run', '--no-default-browser-check', '--disable-default-apps', '--disable-extensions', '--no-proxy-server', '--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1, EXCLUDE ::1'],
   });
   browser = await launching;
   assert.ok(!interrupted);
   const options = {javaScriptEnabled: false, ignoreHTTPSErrors: true, acceptDownloads: false, serviceWorkers: 'block', viewport: {width: 390, height: 844}};
   const guard = async context => {
    await context.route('**/*', async route => {
     const request = route.request();
     const url = new URL(request.url());
     if (url.origin !== origin.origin) { evidence.external++; await route.abort(); return; }
     if (request.method() !== 'GET' && request.method() !== 'HEAD') { evidence.mutations++; await route.abort(); return; }
     await route.continue();
    });
   };
   const context = await browser.newContext(options);
   await guard(context);
   await context.addCookies([{name: '__Host-amos_session', value: input.sessionCookie, url: origin.origin + '/', secure: true, httpOnly: true, sameSite: 'Lax'}]);
   input.sessionCookie = '';
   const page = await context.newPage();
   page.setDefaultTimeout(3000);
   page.setDefaultNavigationTimeout(5000);
   let pageErrors = 0;
   page.on('pageerror', () => { pageErrors++; });
   let cssLoaded = 0;
   page.on('response', response => {
    if (new URL(response.url()).pathname === '/assets/base.css' && response.status() === 200) cssLoaded++;
   });
   const inspect = async (response, source = false) => {
    assert.ok(!interrupted);
    assert.ok(response);
    assert.equal(response.status(), 200);
    assert.equal(await response.headerValue('cache-control'), 'no-store');
    assert.equal(new URL(page.url()).origin, origin.origin);
    assert.equal(await page.locator(source ? '#continuity-source-content' : '#continuity-baseline-content').count(), 1);
    assert.equal(await page.locator('script,iframe').count(), 0);
    assert.equal(await page.locator('link[href="/assets/base.css"]').evaluate(node => node.sheet !== null), true);
    evidence.pages++;
    evidence.navigations++;
    for (const width of [390, 1440]) {
     await page.setViewportSize({width, height: 844});
     const fits = await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);
     assert.equal(fits, true);
     evidence.layouts++;
    }
    await page.setViewportSize({width: 390, height: 844});
   };
   const follow = async locator => {
    const [response] = await Promise.all([page.waitForNavigation({waitUntil: 'load'}), locator.click()]);
    return response;
   };
   const skip = async () => {
    await page.keyboard.press('Tab');
    assert.equal(await page.locator(':focus').textContent(), 'Skip to content');
    await page.keyboard.press('Enter');
    assert.equal(await page.locator(':focus').getAttribute('id'), 'main-content');
    await page.keyboard.press('Tab');
    assert.equal(await page.locator(':focus').getAttribute('href'), '/continuity/properties');
    evidence.keyboard++;
   };
   const forms = async expected => {
    const checked = await page.locator('form[method="post"]').evaluateAll((nodes, expected) =>
     nodes.length === expected && nodes.every(form => {
      const csrf = form.querySelectorAll('input[name="_csrf"]');
      const revision = form.querySelectorAll('input[name="expected"]');
      return csrf.length === 1 && csrf[0].value.length > 0 && revision.length === 1 && revision[0].value === '1';
     }), expected);
    assert.equal(checked, true);
    evidence.forms += expected;
   };

   await inspect(await page.goto(origin.origin + '/continuity/properties', {waitUntil: 'load'})); // 1
   await skip();
   await page.getByLabel('Search properties', {exact: true}).fill('50%_');
   await inspect(await follow(page.getByRole('button', {name: 'Search', exact: true}))); // 2: actual GET form
   assert.equal(new URL(page.url()).searchParams.get('q'), '50%_');
   await inspect(await follow(page.locator('a[href="/continuity/properties/' + input.property + '"]'))); // 3
   assert.equal(await page.getByText('Synthetic occupant', {exact: true}).count(), 1);
   await inspect(await follow(page.getByRole('link', {name: 'Open case register', exact: true}))); // 4
   await inspect(await follow(page.locator('a[href="/continuity/cases/' + input.case + '"]'))); // 5
   assert.equal(await page.locator('h1').textContent(), 'Baseline <script>case</script>');
   await skip();
   await forms(2);
   await inspect(await follow(page.locator('a[href="/continuity/sources/' + input.source + '"]')), true); // 6
   const sourceText = 'Literal 50%_ source <script>untrusted</script>';
   assert.equal((await page.locator('pre').textContent()).trim(), sourceText);
   assert.equal(await page.locator('code').textContent(), crypto.createHash('sha256').update(sourceText).digest('hex'));
   await inspect(await page.goto(origin.origin + '/continuity/cases/' + input.case, {waitUntil: 'load'})); // 7: fresh GET, not a history/cache restoration
   await inspect(await follow(page.getByRole('link', {name: 'Applications', exact: true}))); // 8
   await inspect(await follow(page.locator('a[href="/continuity/applications/' + input.application + '"]'))); // 9
   await skip();
   await forms(1);
   assert.equal(await page.getByRole('button', {name: 'Human decision unavailable', exact: true}).isDisabled(), true);
   await inspect(await follow(page.getByRole('link', {name: 'Procedures', exact: true}))); // 10
   await inspect(await follow(page.locator('a[href="/continuity/procedures/' + input.procedure + '"]'))); // 11
   await skip();
   await forms(1);
   assert.equal((await page.getByLabel('Procedure text', {exact: true}).inputValue()).trim(), 'Plain <script>procedure</script>');
   await inspect(await follow(page.getByRole('link', {name: 'Guide', exact: true}))); // 12
   assert.equal(await page.getByRole('heading', {name: 'Attention in the supplied selection', exact: true}).count(), 1);
   for (const [topic, selected, heading] of [
    ['explain_case', input.case, 'Case explanation'],
    ['owner_draft', input.case, 'Owner update preview'],
    ['spending_authority', '', 'Spending authority'],
    ['handover', '', 'Handover briefing'],
   ]) {
    await page.getByLabel('Guide topic', {exact: true}).selectOption(topic);
    await page.getByLabel('Case (only for case explanation or owner update)', {exact: true}).selectOption(selected);
    await inspect(await follow(page.getByRole('button', {name: 'Show guide', exact: true}))); // 13..16
    assert.equal(await page.getByRole('heading', {name: heading, exact: true}).count(), 1);
   }
   await inspect(await follow(page.getByRole('link', {name: 'Activity', exact: true}))); // 17
   assert.equal(await page.getByText('Case record changed — revision 1;', {exact: false}).count(), 1);
   await inspect(await follow(page.getByRole('link', {name: 'Sources', exact: true})), true); // 18
   await inspect(await follow(page.locator('a[href="/continuity/sources/' + input.source + '"]')), true); // 19
   assert.ok(cssLoaded >= 19);
   assert.equal(pageErrors, 0);
   await context.close();

   const anonymous = await browser.newContext(options);
   await guard(anonymous);
   const deniedPage = await anonymous.newPage();
   const response = await deniedPage.goto(origin.origin + '/continuity/cases/' + input.case, {waitUntil: 'load', timeout: 5000});
   assert.equal(response.status(), 401);
   assert.equal(await response.headerValue('cache-control'), 'no-store');
   const safe = await deniedPage.locator('body').evaluate((body, id) =>
    !body.textContent.includes(id) && !body.textContent.includes('Baseline <script>case</script>') && !body.textContent.includes('Synthetic occupant'), input.case);
   assert.equal(safe, true);
   assert.equal(await deniedPage.locator('form[method="post"]').count(), 0);
   evidence.denied++;
   await anonymous.close();
   assert.equal(evidence.external, 0);
   assert.equal(evidence.mutations, 0);
  })()]);
 } finally {
  clearTimeout(timer);
  if (!browser && launching) {
   try { browser = await launching; } catch { /* Launch failure has no usable browser. */ }
  }
  if (browser) await browser.close();
  process.removeListener('SIGINT', interrupt);
  process.removeListener('SIGTERM', interrupt);
 }
 evidence.ok = true;
 process.stdout.write(JSON.stringify(evidence) + '\n');
})().catch(() => {
 // No exception text: libraries may include selectors, paths or request details.
 process.stdout.write(JSON.stringify({...evidence, ok: false}) + '\n');
 process.exitCode = 1;
});
