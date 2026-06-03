const puppeteer = require('puppeteer');

(async () => {
  const browser = await puppeteer.launch({ headless: true });
  const page = await browser.newPage();
  
  await page.setViewport({ width: 1280, height: 800 });

  page.on('console', msg => console.log('PAGE LOG:', msg.text()));
  page.on('pageerror', error => console.error('PAGE ERROR:', error.message));

  await page.goto('http://localhost:18080/jobs/127');
  await new Promise(r => setTimeout(r, 2000));
  
  await page.click('.action-view-sidebar-list .item'); await new Promise(r => setTimeout(r, 2000)); await page.screenshot({ path: 'screenshot_job.png', fullPage: true });
  
  await browser.close();
})();
