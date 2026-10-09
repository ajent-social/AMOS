// Explicit, static synthetic files only. No server, credentials or form submission.
// node browser.cjs PLAYWRIGHT_MODULE CHROMIUM ARTIFACT_DIR BASE_CSS
const fs = require('node:fs');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const [modulePath, executablePath, directory, css] = process.argv.slice(2);
if (!modulePath || !executablePath || !directory || !css) throw Error('Four explicit paths required');
const {chromium} = require(modulePath);
const digest = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
(async () => {
 const files = ['properties','case','application','procedure','guide','conflict','empty'];
 const stylesheet = fs.readFileSync(css);
 fs.writeFileSync(path.join(directory,'base.css'),stylesheet,{flag:'wx'});
 const records = [];
 for (const name of files) {
  const source = fs.readFileSync(path.join(directory,name+'.html'));
  assert.equal(source.toString().split('href="/assets/base.css"').length,2);
  const staticHTML = source.toString().replace('href="/assets/base.css"','href="./base.css"');
  fs.writeFileSync(path.join(directory,name+'-static.html'),staticHTML,{flag:'wx'});
  records.push({name,sourceDigest:digest(source),staticDigest:digest(staticHTML)});
 }
 const browser = await chromium.launch({executablePath,headless:true,args:['--no-sandbox'],downloadsPath:path.join(directory,'unused-downloads')});
 try {
  const context = await browser.newContext({javaScriptEnabled:false,acceptDownloads:false,viewport:{width:390,height:844}});
  const page = await context.newPage();
  const errors=[],failed=[],requests=[];
  page.on('pageerror',e=>errors.push(e.message));
  page.on('requestfailed',r=>failed.push(r.url()));
  page.on('request',r=>requests.push(r.url()));
  for (const record of records) {
   await page.goto(pathToFileURL(path.join(directory,record.name+'-static.html')).href,{waitUntil:'load'});
   assert.equal(await page.locator('script,img,iframe').count(),0);
   assert.equal(await page.locator('#continuity-baseline-content').count(),1);
   assert.equal(await page.locator('link').evaluate(n=>n.sheet!==null),true);
   await page.keyboard.press('Tab');
   assert.equal(await page.locator(':focus').textContent(),'Skip to content');
   await page.keyboard.press('Enter');
   assert.equal(await page.locator(':focus').getAttribute('id'),'main-content');
   record.keyboard=['Skip to content','main-content'];
   const seen=[];
   for(let i=0;i<35;i++) {
    await page.keyboard.press('Tab');
    const focused=await page.locator(':focus').evaluate(n=>({name:n.getAttribute('name'),tag:n.tagName,disabled:n.disabled===true})).catch(()=>null);
    if(focused) {assert.equal(focused.disabled,false);if(focused.name) seen.push(focused.name);}
   }
   record.keyboardFields=[...new Set(seen)];
   const required={properties:['q'],case:['status','body'],application:['done'],procedure:['body'],guide:['topic','case_id']}[record.name] || [];
   for(const field of required) assert.ok(record.keyboardFields.includes(field),'keyboard cannot reach '+record.name+'/'+field);
   if(record.name==='application') {
    assert.equal(await page.locator('form').count(),1);
    assert.equal(await page.locator('button:disabled').count(),1);
    assert.equal(await page.locator('input[name=item_id]').inputValue(),'01900000-0000-7000-8000-000000000004');
   }
   record.layouts=[];
   for(const [width,size] of [[390,'100%'],[390,'200%'],[1024,'100%'],[1440,'100%'],[1440,'50%']]) {
    await page.setViewportSize({width,height:844});
    // Explicit CSS text scaling, not browser zoom or authenticated acceptance.
    await page.evaluate(size=>document.documentElement.style.fontSize=size,size);
    const geometry=await page.evaluate(()=>({scroll:document.documentElement.scrollWidth,viewport:window.innerWidth}));
    assert.ok(geometry.scroll<=geometry.viewport,'horizontal overflow '+record.name+' '+width+' '+size);
    record.layouts.push({width,textScale:size,...geometry});
   }
   await page.setViewportSize({width:390,height:844});
   await page.evaluate(()=>document.documentElement.style.fontSize='100%');
   await page.screenshot({path:path.join(directory,record.name+'.png'),fullPage:false});
  }
  assert.deepEqual(errors,[]);assert.deepEqual(failed,[]);
  assert.ok(requests.every(u=>u.startsWith(pathToFileURL(directory+path.sep).href)),'request escaped static artifacts');
  fs.writeFileSync(path.join(directory,'browser-evidence.json'),JSON.stringify({status:'PASS',javascript:false,browser:browser.version(),stylesheetDigest:digest(stylesheet),records,errors,failed,requests:requests.length,limitation:'Static synthetic file documents with one explicit local CSS substitution; text scaling is CSS font-size, not browser zoom. No HTTP, form submission, auth, HTMX, state persistence or host acceptance.'},null,2)+'\n',{flag:'wx'});
  await context.close();
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
