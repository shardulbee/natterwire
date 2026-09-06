// Replay actual PTY output in xterm.js, including emoji cluster rendering.
// npm install playwright @xterm/xterm @xterm/addon-unicode-graphemes
// node tests/render.cjs capture.ansi [unicode|wcwidth] [columns] [rows]
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

(async () => {
  const input = path.resolve(process.argv[2]);
  const unicode = process.argv[3] !== 'wcwidth';
  const cols = Number(process.argv[4] || 110);
  const rows = Number(process.argv[5] || 32);
  const browser = await chromium.launch({
    headless: true,
    ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}),
  });
  try {
    const page = await browser.newPage({ viewport: { width: cols * 12 + 40, height: rows * 26 + 40 }, deviceScaleFactor: 1 });
    await page.setContent('<style>body{margin:16px;background:#151719}</style><div id="terminal"></div>');
    await page.addStyleTag({ path: path.resolve(path.dirname(require.resolve('@xterm/xterm')), '../css/xterm.css') });
    await page.addScriptTag({ path: require.resolve('@xterm/xterm') });
    await page.addScriptTag({ path: require.resolve('@xterm/addon-unicode-graphemes') });
    const text = await page.evaluate(async ({ cols, rows, unicode, data }) => {
      const term = new Terminal({ cols, rows, allowProposedApi: true, fontFamily: 'Menlo, monospace', fontSize: 16, lineHeight: 1.3, theme: { background: '#151719', foreground: '#d6d9df', cyan: '#7bd5df', brightBlack: '#858b96' } });
      if (unicode) term.loadAddon(new UnicodeGraphemesAddon.UnicodeGraphemesAddon());
      term.open(document.getElementById('terminal'));
      // These reports come from the recording's PTY; replay does not re-query the app.
      const frames = atob(data).split('\x1b[?2026l');
      for (let i = 0; i < frames.length; i++) {
        const frame = frames[i] + (i < frames.length - 1 ? '\x1b[?2026l' : '');
        await new Promise(resolve => term.write(Uint8Array.from(frame, c => c.charCodeAt(0)), resolve));
        await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      }
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      return Array.from({ length: rows }, (_, y) => term.buffer.active.getLine(y).translateToString(true)).join('\n');
    }, { cols, rows, unicode, data: fs.readFileSync(input).toString('base64') });
    const output = input.replace(/\.ansi$/, '') + (unicode ? '-unicode' : '-wcwidth');
    await page.locator('#terminal').screenshot({ path: output + '.png' });
    fs.writeFileSync(output + '.txt', text);
    console.log(output + '.png');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
