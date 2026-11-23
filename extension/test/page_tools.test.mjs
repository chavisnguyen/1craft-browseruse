import test from 'node:test';
import assert from 'node:assert/strict';

let tab = { id: 1, url: 'about:blank', status: 'complete', windowId: 5 };
const updatedListeners = new Set();
const setTab = (next) => {
  const was = tab.status;
  tab = next;
  if (next.status !== was) updatedListeners.forEach((fn) => fn(1, { status: next.status }));
};
const updates = [];
globalThis.chrome = {
  tabs: {
    async get() {
      return { ...tab };
    },
    async update(id, props) {
      updates.push(props);
      setTimeout(() => setTab({ ...tab, status: 'loading' }), 5);
      setTimeout(() => setTab({ ...tab, status: 'complete', url: props.url }), 40);
    },
    onUpdated: { addListener: (fn) => updatedListeners.add(fn), removeListener: (fn) => updatedListeners.delete(fn) },
    async goBack() {
      setTimeout(() => setTab({ ...tab, status: 'loading' }), 5);
      setTimeout(() => setTab({ ...tab, status: 'complete', url: 'https://back.test/' }), 40);
    },
  },
  onUpdatedHolder: null,
  windows: {
    async update(id, props) {
      updates.push({ window: id, ...props });
    },
  },
};

const tools = await import('../lib/page_tools.js');
const tracker = await import('../lib/tracker.js');

test('navigate normalises the URL and reports arrival', async () => {
  updates.length = 0;
  const res = await tools.navigateTab(1, { url: 'example.com/path' }, { ensureAttached: async () => {} });
  assert.equal(res.output, 'Navigated to https://example.com/path');
  assert.equal(updates[0].url, 'https://example.com/path');
});

test('navigate back / forward', async () => {
  const res = await tools.navigateTab(1, { url: 'back' }, { ensureAttached: async () => {} });
  assert.equal(res.output, 'Navigated back to https://back.test/');
});

test('navigate redirect note and invalid url', async () => {
  chrome.tabs.update = async (id, props) => {
    setTimeout(() => setTab({ ...tab, status: 'loading' }), 5);
    setTimeout(() => setTab({ ...tab, status: 'complete', url: 'https://other.org/landing' }), 40);
  };
  const res = await tools.navigateTab(1, { url: 'https://example.com' }, { ensureAttached: async () => {} });
  assert.match(res.output, /^Navigated to https:\/\/example.com\. The tab was redirected to a page on "https:\/\/other.org"/);
  await assert.rejects(tools.navigateTab(1, { url: 'http://[bad' }, { ensureAttached: async () => {} }), /Invalid URL/);
  await assert.rejects(tools.navigateTab(1, { url: '' }, { ensureAttached: async () => {} }), /URL parameter is required/);
});

test('beforeunload dialog: dismissed -> blocked, force -> accepted', async () => {
  let answered;
  const send = async (tabId, method, p) => {
    answered = p.accept;
  };
  chrome.tabs.update = async () => {
    // a "Leave site?" dialog opens right after the navigation starts
    setTimeout(() => tools.handleBeforeunloadDialog(1, { type: 'beforeunload', url: 'https://old.test/' }, send), 10);
  };
  await assert.rejects(
    tools.navigateTab(1, { url: 'https://new.test' }, { ensureAttached: async () => {} }),
    (e) => e.code === 'navigate_beforeunload_blocked' && /has unsaved changes.*force: true/.test(e.message)
  );
  assert.equal(answered, false);
  chrome.tabs.update = async (id, props) => {
    setTimeout(() => tools.handleBeforeunloadDialog(1, { type: 'beforeunload', url: 'https://old.test/' }, send), 10);
    setTimeout(() => setTab({ ...tab, status: 'loading' }), 20);
    setTimeout(() => setTab({ ...tab, status: 'complete', url: props.url }), 60);
  };
  const res = await tools.navigateTab(1, { url: 'https://new.test', force: true }, { ensureAttached: async () => {} });
  assert.equal(answered, true);
  assert.match(res.output, /discarded a "Leave site\?" dialog/);
});

test('javascript_tool result formatting and errors', async () => {
  tab = { ...tab, url: 'https://x.test/' };
  const mk = (result, exceptionDetails) => ({ async sendCommand() { return { result, exceptionDetails }; } });
  const run = (deps, text = 'x') => tools.runJavascript(1, { action: 'javascript_exec', text }, deps);
  assert.equal((await run(mk({ type: 'number', value: 42 }))).output, '42');
  assert.equal((await run(mk({ type: 'string', value: 'hi' }))).output, 'hi');
  assert.equal((await run(mk({ type: 'undefined' }))).output, 'undefined');
  assert.equal((await run(mk({ type: 'object', subtype: 'null' }))).output, 'null');
  assert.equal((await run(mk({ type: 'object', value: { a: 1 } }))).output, '{\n  "a": 1\n}');
  assert.equal((await run(mk({ type: 'object', subtype: 'array', value: [1, 2] }))).output, '[\n  1,\n  2\n]');
  assert.equal((await run(mk({ type: 'function', description: 'function f(){}' }))).output, 'function f(){}');
  await assert.rejects(run(mk(undefined, { exception: { description: 'ReferenceError: nope is not defined' } })), (e) => e.code === 'js_execution_error' && e.message === 'JavaScript execution error: ReferenceError: nope is not defined');
  await assert.rejects(run(mk(undefined, { exception: { description: 'Error: execution was terminated' } })), (e) => e.code === 'js_timeout' && /30-second limit/.test(e.message));
  await assert.rejects(tools.runJavascript(1, { action: 'x', text: '1' }, mk({})), /only supported action/);
  await assert.rejects(tools.runJavascript(1, { action: 'javascript_exec' }, mk({})), /Code parameter is required/);
  const long = await run(mk({ type: 'string', value: 'a'.repeat(60000) }));
  assert.match(long.output, /\[OUTPUT TRUNCATED: Exceeded 50KB limit\]$/);
  tab = { ...tab, url: 'chrome://settings/' };
  await assert.rejects(run(mk({})), (e) => e.code === 'chrome_protected_page' && /chrome:\/\/ pages/.test(e.message));
});

test('javascript_tool retries "Illegal return statement" as an async function', async () => {
  tab = { ...tab, url: 'https://x.test/' };
  const calls = [];
  const deps = {
    async sendCommand(id, method, p) {
      calls.push(p);
      if (calls.length === 1) return { exceptionDetails: { exception: { className: 'SyntaxError', description: 'SyntaxError: Illegal return statement' } } };
      return { result: { type: 'number', value: 7 } };
    },
  };
  const res = await tools.runJavascript(1, { action: 'javascript_exec', text: 'return 7' }, deps);
  assert.equal(res.output, '7');
  assert.equal(calls[0].replMode, true);
  assert.match(calls[1].expression, /^\(async\(\)=>\{/);
});

test('resize_window validation', async () => {
  updates.length = 0;
  const ok = await tools.resizeWindow(1, { width: 800.7, height: 600 });
  assert.equal(ok.output, 'Successfully resized window containing tab 1 to 800x600 pixels');
  assert.equal(updates[0].width, 800);
  await assert.rejects(tools.resizeWindow(1, { width: 9000, height: 600 }), /exceed 8K resolution limit/);
  await assert.rejects(tools.resizeWindow(1, { width: -1, height: 600 }), /positive numbers/);
  await assert.rejects(tools.resizeWindow(1, { height: 600 }), /Both width and height/);
});

test('file_upload validation', async () => {
  await assert.rejects(tools.fileUpload(1, { ref: 'ref_1' }), /files parameter is required/);
  await assert.rejects(tools.fileUpload(1, { ref: 'ref_1', files: [{ data: 'AA' }] }), /must have `data` and `name`/);
  await assert.rejects(tools.fileUpload(1, { ref: 'ref_1', files: [{ data: 'AA', name: '../x.png' }] }), /plain file name/);
  await assert.rejects(tools.fileUpload(1, { ref: 'ref_1', files: [{ data: 'A'.repeat(15 * 1024 * 1024), name: 'big.bin' }] }), /exceeds 10 MB/);
  await assert.rejects(tools.fileUpload(1, { files: [{ data: 'AA', name: 'a.png' }] }), /ref parameter is required/);
  await assert.rejects(tools.uploadImage(1, { imageId: 'ss_x', ref: 'ref_1', coordinate: [1, 2] }), /not both/);
  await assert.rejects(tools.uploadImage(1, { imageId: 'ss_x' }), /Either ref or coordinate/);
  await assert.rejects(tools.uploadImage(1, { imageId: 'ss_nope', ref: 'ref_1' }), (e) => e.code === 'screenshot_bytes_unavailable');
});

test('console tracker: capture, filter, format, domain reset', () => {
  const T = 7;
  tracker.onDebuggerEvent(T, 'Page.frameNavigated', { frame: { url: 'https://a.test/' } });
  tracker.onDebuggerEvent(T, 'Runtime.consoleAPICalled', { type: 'log', args: [{ value: 'hello' }, { value: 3 }], stackTrace: { callFrames: [{ url: 'https://a.test/app.js', lineNumber: 9, columnNumber: 4 }] } });
  tracker.onDebuggerEvent(T, 'Runtime.consoleAPICalled', { type: 'error', args: [{ description: 'Error: boom' }] });
  tracker.onDebuggerEvent(T, 'Runtime.exceptionThrown', { exceptionDetails: { text: 'Uncaught', exception: { description: 'TypeError: x is undefined' }, url: 'https://a.test/app.js', lineNumber: 1, columnNumber: 2 } });
  assert.equal(tracker.getConsoleMessages(T, false).length, 3);
  assert.equal(tracker.getConsoleMessages(T, true).length, 2);
  assert.equal(tracker.getConsoleMessages(T, false, 'hel+o').length, 1);
  const out = tracker.formatConsole(tracker.getConsoleMessages(T, false), { limit: 2 });
  assert.match(out, /^Found 3 console messages \(showing first 2 of 3\):\n\n\[1\] \[[^\]]+\] \[LOG\] \(https:\/\/a\.test\/app\.js:9:4\)\nhello 3/);
  assert.match(tracker.formatConsole([], { onlyErrors: true }), /^No console errors or exceptions found for this tab\./);
  assert.throws(() => tracker.getConsoleMessages(T, false, '('), /Invalid regex/);
  tracker.onDebuggerEvent(T, 'Page.frameNavigated', { frame: { url: 'https://a.test/other' } });
  assert.equal(tracker.getConsoleMessages(T, false).length, 3);
  tracker.onDebuggerEvent(T, 'Page.frameNavigated', { frame: { url: 'https://b.test/' } });
  assert.equal(tracker.getConsoleMessages(T, false).length, 0);
});

test('network tracker: status, failures, pattern, format', () => {
  const T = 8;
  tracker.onDebuggerEvent(T, 'Network.requestWillBeSent', { requestId: '0', request: { url: 'https://a.test/before-tracking', method: 'GET' } });
  assert.equal(tracker.getNetworkRequests(T).length, 0);
  assert.equal(tracker.enableNetworkTracking(T), false);
  tracker.onDebuggerEvent(T, 'Network.requestWillBeSent', { requestId: '1', request: { url: 'https://a.test/api/x', method: 'GET' } });
  tracker.onDebuggerEvent(T, 'Network.responseReceived', { requestId: '1', response: { status: 200 } });
  tracker.onDebuggerEvent(T, 'Network.requestWillBeSent', { requestId: '2', request: { url: 'https://a.test/img.png', method: 'GET' } });
  tracker.onDebuggerEvent(T, 'Network.loadingFailed', { requestId: '2', errorText: 'net::ERR_BLOCKED' });
  tracker.onDebuggerEvent(T, 'Network.requestWillBeSent', { requestId: '3', request: { url: 'https://a.test/slow', method: 'POST' } });
  const all = tracker.getNetworkRequests(T);
  const out = tracker.formatNetwork(all, {});
  assert.match(out, /^Found 3 network requests:\n\n1\. url: https:\/\/a\.test\/api\/x\n   method: GET\n   statusCode: 200\n\n2\. url: https:\/\/a\.test\/img\.png\n   method: GET\n   statusCode: none\n   failed: net::ERR_BLOCKED\n\n3\. url: https:\/\/a\.test\/slow\n   method: POST\n   statusCode: pending$/);
  assert.match(tracker.formatNetwork([all[0]], { urlPattern: '/api/' }), /^Found 1 network request \(filtered by URL pattern: "\/api\/"\):/);
  assert.match(tracker.formatNetwork([], { urlPattern: 'zzz' }), /^No requests matching "zzz" found for this tab\./);
});

test('navigating to the page that is already open still completes (fast loading phase)', async () => {
  tab = { id: 1, url: 'https://same.test/', status: 'complete', windowId: 5 };
  chrome.tabs.update = async (id, props) => {
    // loading flips on and off within one polling interval
    setTimeout(() => {
      setTab({ ...tab, status: 'loading' });
      setTab({ ...tab, status: 'complete', url: props.url });
    }, 10);
  };
  const res = await tools.navigateTab(1, { url: 'https://same.test/' }, { ensureAttached: async () => {} });
  assert.equal(res.output, 'Navigated to https://same.test/');
});

test('chrome API failures are prefixed like Claude', async () => {
  chrome.tabs.goBack = async () => {
    throw new Error('Cannot find a next page in history.');
  };
  await assert.rejects(tools.navigateTab(1, { url: 'back' }, { ensureAttached: async () => {} }), /^.*Failed to navigate: Cannot find a next page in history\./);
});
