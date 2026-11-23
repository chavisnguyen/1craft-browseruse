// Run with: node --test extension/test
import test from 'node:test';
import assert from 'node:assert/strict';

const calls = [];
let viewport = { width: 1280, height: 800, dpr: 2, scrollX: 0, scrollY: 0 };
let refResult = { success: true, coordinates: [200, 100], hasBox: true, view: 'v' };
globalThis.chrome = {
  scripting: {
    async executeScript(opts) {
      if (opts.files) return [];
      const src = String(opts.func);
      if (src.includes('innerWidth')) return [{ result: viewport }];
      if (src.includes('__klikResolveRef')) return [{ result: refResult }];
      return [{ result: undefined }];
    },
  },
  tabs: {
    onCreated: { addListener() {}, removeListener() {} },
  },
};

const { runComputerAction, fitToBudget } = await import('../lib/computer.js');
const { parseKeyToken, isPageZoomShortcut, parseModifierList } = await import('../lib/keys.js');

const deps = {
  async sendCommand(tabId, method, params) {
    calls.push({ tabId, method, params });
    if (method === 'Page.captureScreenshot') return { data: 'AAAA' };
    return {};
  },
  groupTabsSeq: null,
  async groupTabIds() {
    return this.groupTabsSeq ? this.groupTabsSeq.shift() : [7];
  },
};

test('fitToBudget keeps small viewports and shrinks large ones within budget', () => {
  assert.deepEqual(fitToBudget(1280, 800), [1280, 800]);
  const [w, h] = fitToBudget(2560, 1600);
  assert.ok(w <= 1568 && h <= 1568);
  assert.ok(Math.ceil(w / 28) * Math.ceil(h / 28) <= 1568);
  assert.ok(Math.abs(w / h - 1.6) < 0.02);
});

test('key parsing: shortcuts, commands, zoom rejection', () => {
  const sel = parseKeyToken('cmd+a');
  assert.equal(sel.modifiers, 4);
  assert.deepEqual(sel.commands, ['selectAll']);
  assert.equal(sel.text, undefined);
  assert.equal(parseKeyToken('Enter').vk, 13);
  assert.equal(parseKeyToken('ctrl+shift+Tab').modifiers, 10);
  assert.ok(isPageZoomShortcut('cmd+='));
  assert.ok(isPageZoomShortcut('ctrl+-'));
  assert.ok(!isPageZoomShortcut('ctrl+a'));
  assert.equal(parseModifierList('ctrl+shift'), 10);
  assert.throws(() => parseKeyToken('nonsense'), /Unknown key/);
});

test('click uses the screenshot coordinate frame and modifiers', async () => {
  calls.length = 0;
  const shot = await runComputerAction(7, { action: 'screenshot' }, deps);
  assert.match(shot.output, /Successfully captured screenshot/);
  assert.match(shot.imageId, /^ss_/);
  const ctx = calls.find((c) => c.method === 'Page.captureScreenshot').params;
  assert.equal(ctx.format, 'jpeg');
  calls.length = 0;
  // frame is (fit width) wide for a 1280-wide viewport: x=frameWidth/2 maps to 640
  const frameW = fitToBudget(2560, 1600)[0];
  await runComputerAction(7, { action: 'left_click', coordinate: [frameW / 2, 10], modifiers: 'ctrl' }, deps);
  const pressed = calls.find((c) => c.method === 'Input.dispatchMouseEvent' && c.params.type === 'mousePressed');
  assert.equal(pressed.params.x, 640);
  assert.equal(pressed.params.modifiers, 2);
  assert.equal(pressed.params.clickCount, 1);
});

test('out-of-frame coordinates are rejected', async () => {
  await assert.rejects(
    runComputerAction(7, { action: 'left_click', coordinate: [99999, 5] }, deps),
    (e) => e.code === 'coordinate_out_of_bounds'
  );
});

test('click by ref uses the element centre', async () => {
  calls.length = 0;
  await runComputerAction(8, { action: 'double_click', ref: 'ref_3' }, deps);
  const presses = calls.filter((c) => c.params && c.params.type === 'mousePressed');
  assert.equal(presses.length, 2);
  assert.equal(presses[1].params.clickCount, 2);
  assert.equal(presses[0].params.x, 200);
});

test('a click that opens a tab reports the new tab id', async () => {
  deps.groupTabsSeq = [[8], [8, 99]];
  const res = await runComputerAction(8, { action: 'left_click', ref: 'ref_3' }, deps);
  deps.groupTabsSeq = null;
  assert.match(res.output, /^Clicked on element ref_3 \[note: the link opened in a new tab \(tab ID 99\)/);
});

test('stale or hidden refs send no mouse event', async () => {
  calls.length = 0;
  refResult = { success: false, error: 'No element found with reference: "ref_9".' };
  await assert.rejects(runComputerAction(8, { action: 'left_click', ref: 'ref_9' }, deps), (e) => e.code === 'computer_element_not_found');
  refResult = { success: true, coordinates: [1, 1], hasBox: false };
  await assert.rejects(runComputerAction(8, { action: 'hover', ref: 'ref_9' }, deps), /No mouse event was sent/);
  assert.equal(calls.filter((c) => c.method === 'Input.dispatchMouseEvent').length, 0);
});

test('key action: repeat, sequence, zoom shortcut error', async () => {
  calls.length = 0;
  const res = await runComputerAction(7, { action: 'key', text: 'ArrowDown Enter', repeat: 3 }, deps);
  assert.match(res.output, /Pressed 2 keys: ArrowDown Enter \(repeated 3 times\)/);
  assert.equal(calls.filter((c) => c.params.type === 'keyUp').length, 6);
  await assert.rejects(runComputerAction(7, { action: 'key', text: 'cmd+=' }, deps), (e) => e.code === 'page_zoom_shortcut_unsupported');
  await assert.rejects(runComputerAction(7, { action: 'key', text: 'a', repeat: 101 }, deps), /cannot exceed 100/);
});

test('type, wait, scroll validation', async () => {
  calls.length = 0;
  const t = await runComputerAction(7, { action: 'type', text: 'hi\nthere' }, deps);
  assert.equal(t.output, 'Typed "hi\nthere"');
  assert.deepEqual(calls.filter((c) => c.method === 'Input.insertText').map((c) => c.params.text), ['hi', 'there']);
  await assert.rejects(runComputerAction(7, { action: 'wait', duration: 11 }, deps), /cannot exceed 10/);
  const w = await runComputerAction(7, { action: 'wait', duration: 0.01 }, deps);
  assert.match(w.output, /Waited for 0.01 seconds/);
  await assert.rejects(runComputerAction(7, { action: 'scroll', coordinate: [1, 1], scroll_direction: 'sideways' }, deps), /Invalid scroll direction/);
  await assert.rejects(runComputerAction(7, { action: 'middle_click' }, deps), /Unsupported action/);
});

test('scroll sends a wheel event and a follow-up screenshot', async () => {
  calls.length = 0;
  const res = await runComputerAction(7, { action: 'scroll', coordinate: [10, 10], scroll_direction: 'down', scroll_amount: 2 }, deps);
  const wheel = calls.find((c) => c.params.type === 'mouseWheel');
  assert.equal(wheel.params.deltaY, 200);
  assert.match(res.output, /Scrolled down by 2 ticks at \(10, 10\)/);
  assert.ok(res.base64Image);
});

test('drag and zoom', async () => {
  calls.length = 0;
  const d = await runComputerAction(7, { action: 'left_click_drag', start_coordinate: [10, 10], coordinate: [100, 100] }, deps);
  assert.match(d.output, /Dragged from \(10, 10\) to \(100, 100\)/);
  const z = await runComputerAction(7, { action: 'zoom', region: [0, 0, 100, 100], scale: 0.5 }, deps);
  assert.match(z.output, /Successfully captured zoomed screenshot of region \(0,0\) to \(100,100\)/);
  await assert.rejects(runComputerAction(7, { action: 'zoom', region: [10, 10, 5, 5] }, deps), /Invalid region/);
});
