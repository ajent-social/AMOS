// Static synthetic file inspection only. No server, downloads or existing data.
// Usage: node browser.cjs PLAYWRIGHT_MODULE CHROMIUM ARTIFACT_DIR BASE_CSS
const fs = require('node:fs');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const [modulePath, executablePath, output, css] = process.argv.slice(2);
if (!modulePath || !executablePath || !output || !css) throw new Error('Four explicit paths required');
const {chromium} = require(modulePath);
const digest = b => crypto.createHash('sha256').update(b).digest('hex');
(async () => {
 const stylesheet = fs.readFileSync(css);
 fs.copyFileSync(css, path.join(output, 'base.css'));
 const records=[];
 for (const name of ['detail','list']) {
  const raw=fs.readFileSync(path.join(output,name+'.html'));
  const original=raw.toString('utf8');
  assert.equal(original.split('href="/assets/base.css"').length,2);
  const staticHTML=original.replace('href="/assets/base.css"','href="./base.css"');
  fs.writeFileSync(path.join(output,name+'-static.html'),staticHTML);
  records.push({name, original_sha256:digest(raw), static_sha256:digest(staticHTML)});
 }
 const browser=await chromium.launch({executablePath,headless:true,args:['--no-sandbox'],downloadsPath:path.join(output,'unused-downloads')});
 try {
  const context=await browser.newContext({javaScriptEnabled:false,viewport:{width:360,height:780},acceptDownloads:false});
  const page=await context.newPage();
  const errors=[];const failed=[];const requests=[];
  page.on('pageerror',e=>errors.push(e.message));
  page.on('requestfailed',r=>failed.push({url:r.url(),failure:r.failure()}));
  page.on('request',r=>requests.push(r.url()));
  for (const record of records) {
   await page.goto(pathToFileURL(path.join(output,record.name+'-static.html')).href,{waitUntil:'load'});
   assert.equal(await page.locator('script,img,iframe').count(),0);
   assert.equal(await page.locator('section#continuity-source-content').count(),1);
   assert.equal(await page.locator('link').evaluate(n=>n.sheet!==null),true);
   record.width=await page.evaluate(()=>({scroll:document.documentElement.scrollWidth,viewport:window.innerWidth}));
   assert.ok(record.width.scroll<=record.width.viewport,'horizontal overflow');
   await page.keyboard.press('Tab');
   assert.equal(await page.locator(':focus').textContent(),'Skip to content');
   record.first_focus='Skip to content';
   await page.keyboard.press('Enter');
   assert.equal(await page.locator(':focus').getAttribute('id'),'main-content');
   record.skip_activation='main-content';
   if(record.name==='detail') {
    const expected='\n'+'long<&>text'.repeat(5000)+'\n';
    assert.equal(await page.locator('pre').textContent(),expected);
    record.body_sha256=digest(expected);
    await page.keyboard.press('Tab');
    assert.equal(await page.locator(':focus').textContent(),'Back to sources');
    record.second_focus='Back to sources';
   } else {
    await page.keyboard.press('Tab');assert.equal(await page.locator(':focus').getAttribute('name'),'q');
    assert.equal(await page.locator('[name=q]').inputValue(),'<script> & +');
    await page.keyboard.press('Tab');assert.equal(await page.locator(':focus').getAttribute('name'),'kind');
    await page.keyboard.press('Tab');assert.equal(await page.locator(':focus').textContent(),'Search');
    const next=await page.getByText('Try next page',{exact:true}).getAttribute('href');
    const u=new URL(next,'https://synthetic.invalid');
    assert.equal(u.searchParams.get('q'),'<script> & +');
    assert.equal(u.searchParams.get('limit'),'1');
    record.focus_order=['Skip to content','q','kind','Search'];
   }
   await page.screenshot({path:path.join(output,record.name+'.png'),fullPage:false});
  }
  assert.deepEqual(errors,[]);assert.deepEqual(failed,[]);
  assert.ok(requests.every(u=>u.startsWith(pathToFileURL(output+path.sep).href)),'non-artifact request');
  fs.writeFileSync(path.join(output,'browser-evidence.json'),JSON.stringify({status:'PASS',javascript:false,viewport:{width:360,height:780},browser_version:browser.version(),stylesheet_sha256:digest(stylesheet),records,errors,failed,request_count:requests.length,limitation:'Static file artifacts with explicit local stylesheet substitution only; no route/form submission, HTTP, HTMX, authority or authenticated journey qualification.'},null,2)+'\n');
  await context.close();
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
