import { expect, test } from '@playwright/test';

for(const shell of ['classic','ervisios']){
  test(`Photos opens the shared file browser and filters media in ${shell}`,async({page})=>{
    await page.addInitScript(shell=>{
      localStorage.setItem('ervisio.mock.user',JSON.stringify('alice'));
      localStorage.setItem('ervisio.mock.home',JSON.stringify('/srv/media'));
      localStorage.setItem('ervisio.mock.prefs',JSON.stringify({'shell.mode':shell,language:'en'}));
      const entry=(name:string,mime:string)=>({name,mime,type:'file',size:12,mtime:1,mode:'0644',perm:'rw-r--r--',owner:'alice',group:'alice'});
      localStorage.setItem('ervisio.mock.files',JSON.stringify({'/srv/media':[entry('photo.svg','image/svg+xml'),entry('notes.txt','text/plain')]}));
    },shell);
    await page.route('**/api/files/media?**',route=>route.fulfill({contentType:'image/svg+xml',body:'<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="skyblue"/></svg>'}));
    await page.goto('/media');
    await page.getByRole('button',{name:'Open file',exact:true}).first().click();
    const picker=page.getByRole('dialog',{name:'Open a photo, video or audio file',exact:true});
    await expect(picker.getByRole('option').filter({hasText:'photo.svg'})).toBeVisible();
    await expect(picker.getByRole('option').filter({hasText:'notes.txt'})).toHaveCount(0);
    await picker.getByRole('option').filter({hasText:'photo.svg'}).dblclick();
    await expect(picker).toHaveCount(0);
    await expect(page.locator('.media-viewer img[alt="photo.svg"]')).toBeVisible();
  });

  test(`Software activity recovers progress, notifications and completed logs in ${shell}`,async({page})=>{
    await page.addInitScript(shell=>{
      if(localStorage.getItem('activity.fixture'))return;localStorage.setItem('activity.fixture','1');
      localStorage.setItem('ervisio.mock.user',JSON.stringify('alice'));
      localStorage.setItem('ervisio.mock.prefs',JSON.stringify({'shell.mode':shell,language:'en'}));
      localStorage.setItem('activity.phase','running');
    },shell);
    await page.route('**/src/api/mock.ts',async route=>{
      const response=await route.fetch();const source=await response.text();
      const injection=`
      if(method==='software.status'){const running=localStorage.getItem('activity.phase')==='running';return {busy:running,transaction:{running,ok:!running,op:'install',source:'cli',packages:['claude-code'],startedAt:123456,finishedAt:running?0:Date.now(),step:1,steps:2,done:1,total:2,current:'Node.js LTS',log:['Downloading official Node.js LTS','SHA-256 verified; extracting node and npm'],message:running?'':'Claude Code installed',hint:'',rebootNeeded:false}};}
      if(method==='software.summary')return {manager:'apt',sources:['repo'],updates:0,lastCheck:Date.now(),counts:{repo:0},warnings:[]};
      if(['software.updates','software.installed','software.apps','software.suggest'].includes(method))return [];`;
      const body=source.replace(/(async function mockCall\([^)]*\)\s*\{)/,`$1${injection}`);expect(body).not.toBe(source);await route.fulfill({response,body});
    });
    await page.goto('/software?tab=store');
    const panel=page.locator('.ui-panel').filter({has:page.getByLabel('Output',{exact:true})});
    await expect(panel).toBeVisible();
    await panel.getByRole('button',{name:'Hide',exact:true}).click();
    await expect(panel).toBeHidden();
    await page.locator('.sw-bar').getByRole('button',{name:/Activity/}).click();
    const activity=page.getByRole('region',{name:'Activity',exact:true});
    await expect(activity).toContainText('SHA-256 verified');
    await page.evaluate(()=>localStorage.setItem('activity.phase','done'));
    await expect(activity).toContainText('Claude Code installed');
    await page.reload();
    if(shell==='ervisios')await page.goto('/software?tab=activity');
    await expect(page.getByRole('region',{name:'Activity',exact:true})).toContainText('SHA-256 verified');
    await expect(page.getByRole('region',{name:'Activity',exact:true})).toContainText('Completed');
  });
}
