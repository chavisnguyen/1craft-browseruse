import { ToolError } from './lib/errors.js';
import { runComputerAction, forgetTabContext, captureFrame, frameToViewport, ui, summaryLabel } from './lib/computer.js';
import { onDebuggerEvent, enableNetworkTracking, forgetTab, getConsoleMessages, clearConsole, getNetworkRequests, clearNetwork, formatConsole, formatNetwork } from './lib/tracker.js';
import { handleBeforeunloadDialog, navigateTab, runJavascript, resizeWindow, fileUpload, uploadImage } from './lib/page_tools.js';
import * as gif from './lib/gif.js';
import { findLocal } from './lib/find_match.js';
import { formInputInPage } from './lib/form_input.js';
import { extractPageText } from './lib/page_text.js';

let ws = null;
let pingInterval = null;
let reconnectTimeout = null;
let reconnectDelay = 1000;
const MAX_RECONNECT_DELAY = 30000;

let agentGroupId = null;
const attachedDebuggers = new Set();

function generateUUID() {
  if (crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

function setBadge(text, color) {
  chrome.action.setBadgeText({ text });
  if (color) {
    chrome.action.setBadgeBackgroundColor({ color });
  }
}

export function deriveLabelFromEmail(email) {
  if (!email || !email.includes('@')) return '';
  const parts = email.trim().toLowerCase().split('@');
  const user = parts[0];
  const domainFull = parts[1] || '';
  const domain = domainFull.split('.')[0];
  if (!domain || !user) return '';
  let candidate = `${domain}_${user}`;
  candidate = candidate.replace(/[^a-z0-9_.-]/g, '_');
  if (candidate.length > 32) {
    candidate = candidate.slice(0, 32);
  }
  return candidate;
}

async function getConfig() {
  const data = await chrome.storage.local.get([
    'label',
    'token',
    'daemonUrl',
    'windowMode',
    'instanceId',
    'email',
  ]);

  let label = (data.label || '').trim();
  let token = (data.token || '').trim();
  let daemonUrl = (data.daemonUrl || 'ws://127.0.0.1:3025/ext').trim();
  let windowMode = data.windowMode || 'current';

  if (!token) {
    try {
      const resp = await fetch(chrome.runtime.getURL('default_config.json'));
      if (resp.ok) {
        const def = await resp.json();
        if (def.token) {
          token = (def.token || '').trim();
          if (!label) {
            label = (def.label || 'default').trim();
          }
          daemonUrl = (def.daemonUrl || daemonUrl).trim();
          windowMode = def.windowMode || windowMode;
          await chrome.storage.local.set({ label, token, daemonUrl, windowMode });
        }
      }
    } catch {}
  }

  if (!label || label === 'default') {
    try {
      if (chrome.identity && chrome.identity.getProfileUserInfo) {
        const info = await new Promise((resolve) => {
          chrome.identity.getProfileUserInfo({ accountStatus: 'ANY' }, (res) => {
            resolve(res);
          });
        });
        if (info && info.email) {
          const derived = deriveLabelFromEmail(info.email);
          if (derived) {
            label = derived;
            await chrome.storage.local.set({ label, email: info.email });
          }
        }
      }
    } catch (e) {
      console.warn('Could not derive label from email:', e);
    }
  }

  let instanceId = data.instanceId;
  if (!instanceId) {
    instanceId = generateUUID();
    await chrome.storage.local.set({ instanceId });
  }
  return {
    label,
    token,
    daemonUrl,
    windowMode,
    instanceId,
  };
}

// ---------------------------------------------------------------------------
// MCP tab group (mirrors Claude in Chrome's TabGroupManager.getOrCreateMcpTabContext)
// Tools may only act on tabs inside this group. The group id survives service
// worker restarts because it is looked up again by title.
// ---------------------------------------------------------------------------
const MCP_GROUP_TITLE = 'klik';
const NO_GROUP_NOTE =
  'Any tabs opened earlier in this conversation may still be open in Chrome, outside any group these tools can see or control.';

// The MCP group is whatever Chrome tab group carries our title (and still has tabs). Nothing is
// persisted: the service worker can restart or the extension reload and we simply look it up again.
async function loadMcpGroupId() {
  if (agentGroupId !== null) {
    try {
      await chrome.tabGroups.get(agentGroupId);
      return agentGroupId;
    } catch {
      agentGroupId = null;
    }
  }
  try {
    const groups = await chrome.tabGroups.query({ title: MCP_GROUP_TITLE });
    for (const group of groups) {
      const tabs = await chrome.tabs.query({ groupId: group.id });
      if (tabs.length > 0) {
        agentGroupId = group.id;
        break;
      }
    }
  } catch {}
  return agentGroupId;
}

function toTabSummary(t) {
  return { tabId: t.id, title: t.title || '', url: t.url || '' };
}

async function listGroupTabs(groupId) {
  const tabs = await chrome.tabs.query({ groupId });
  return tabs.filter((t) => t.id !== undefined).map(toTabSummary);
}

// Returns { tabGroupId, availableTabs } or null when no group exists (and createIfEmpty is false).
async function getOrCreateMcpTabContext({ createIfEmpty = false } = {}) {
  await loadMcpGroupId();
  if (agentGroupId !== null) {
    try {
      await chrome.tabGroups.get(agentGroupId);
      const availableTabs = await listGroupTabs(agentGroupId);
      if (availableTabs.length > 0) {
        return { tabGroupId: agentGroupId, availableTabs };
      }
    } catch {
      // group is gone
    }
    agentGroupId = null;
  }
  if (!createIfEmpty) return null;

  const config = await getConfig();
  let tabId;
  if (config.windowMode !== 'dedicated') {
    try {
      const win = await chrome.windows.getLastFocused({ windowTypes: ['normal'] });
      const tab = await chrome.tabs.create({ windowId: win.id, url: 'chrome://newtab/', active: false });
      tabId = tab.id;
    } catch {
      tabId = undefined;
    }
  }
  if (tabId === undefined) {
    const win = await chrome.windows.create({
      url: 'chrome://newtab/',
      focused: false,
      type: 'normal',
      width: 1280,
      height: 900,
    });
    tabId = win && win.tabs && win.tabs[0] && win.tabs[0].id;
  }
  if (tabId === undefined) throw new ToolError('tab_create_exception', 'Failed to create a tab for the MCP tab group');

  agentGroupId = await chrome.tabs.group({ tabIds: [tabId] });
  await chrome.tabGroups.update(agentGroupId, { title: MCP_GROUP_TITLE, color: 'orange' });
  const tab = await chrome.tabs.get(tabId);
  return { tabGroupId: agentGroupId, availableTabs: [toTabSummary(tab)] };
}

// Throws unless tabId belongs to the MCP tab group.
async function assertTabInMcpGroup(tabId) {
  await loadMcpGroupId();
  const notInGroup = () =>
    new ToolError(
      'tab_outside_group',
      `Tab ${tabId} is not in Claude's tab group for this session. Tools can only target tabs inside the group. Call tabs_context_mcp to list valid tabs (or create the group with createIfEmpty: true), or tabs_create_mcp to add a tab.`
    );
  if (agentGroupId === null) throw notInGroup();
  let tab;
  try {
    tab = await chrome.tabs.get(tabId);
  } catch {
    throw new ToolError('tab_not_found', `Tab ${tabId} does not exist (may have already been closed). Call tabs_context_mcp to see current tabs.`);
  }
  if (tab.groupId !== agentGroupId) throw notInGroup();
  return tab;
}

async function buildTabContext(currentTabId) {
  await loadMcpGroupId();
  if (agentGroupId === null) return undefined;
  try {
    const availableTabs = await listGroupTabs(agentGroupId);
    return {
      currentTabId,
      executedOnTabId: currentTabId,
      availableTabs,
      tabCount: availableTabs.length,
      tabGroupId: agentGroupId,
    };
  } catch {
    return undefined;
  }
}

// Debugger lifecycle management with Runtime, Page (anti-dialog hang), and Network listeners
async function ensureDebuggerAttached(tabId) {
  if (attachedDebuggers.has(tabId)) {
    return true;
  }
  try {
    await chrome.debugger.attach({ tabId }, '1.3');
    attachedDebuggers.add(tabId);
    try {
      await chrome.debugger.sendCommand({ tabId }, 'Runtime.enable');
      await chrome.debugger.sendCommand({ tabId }, 'Page.enable');
      // Background tabs must behave as focused so input events and screenshots work.
      await chrome.debugger.sendCommand({ tabId }, 'Emulation.setFocusEmulationEnabled', { enabled: true });
    } catch {}
    return true;
  } catch (err) {
    console.warn(`Debugger attach failed on tab ${tabId}:`, err);
    return false;
  }
}

const rawSend = (tabId, method, params) => chrome.debugger.sendCommand({ tabId }, method, params);

chrome.debugger.onEvent.addListener(async (source, method, params) => {
  if (!source || !source.tabId) return;
  const tabId = source.tabId;

  // Dialogs must be answered or the page hangs: "Leave site?" follows the navigate `force` policy,
  // alert/confirm/prompt are accepted.
  if (method === 'Page.javascriptDialogOpening') {
    if (await handleBeforeunloadDialog(tabId, params, rawSend)) return;
    try {
      await rawSend(tabId, 'Page.handleJavaScriptDialog', { accept: true, promptText: '' });
    } catch {}
    return;
  }

  onDebuggerEvent(tabId, method, params);
});

chrome.debugger.onDetach.addListener((source) => {
  if (source.tabId) {
    attachedDebuggers.delete(source.tabId);
  }
});

chrome.tabs.onRemoved.addListener((tabId) => {
  forgetTab(tabId);
  forgetTabContext(tabId);
  if (attachedDebuggers.has(tabId)) {
    try {
      chrome.debugger.detach({ tabId });
    } catch {}
    attachedDebuggers.delete(tabId);
  }
});

function isRestrictedUrl(url) {
  if (!url) return false;
  return (
    url.startsWith('chrome://') ||
    url.startsWith('chrome-extension://') ||
    url.startsWith('https://chromewebstore.google.com')
  );
}

function waitForTabLoad(tabId, timeoutMs = 20000) {
  return new Promise((resolve) => {
    let timer = null;
    const listener = (updatedTabId, changeInfo, tab) => {
      if (updatedTabId === tabId && changeInfo.status === 'complete') {
        chrome.tabs.onUpdated.removeListener(listener);
        if (timer) clearTimeout(timer);
        resolve(tab);
      }
    };
    timer = setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      resolve(null);
    }, timeoutMs);
    chrome.tabs.onUpdated.addListener(listener);
  });
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function injectPageTree(tabId) {
  await chrome.scripting.executeScript({ target: { tabId }, files: ['lib/page_tree.js'], injectImmediately: true });
}

// ---- GIF recording -------------------------------------------------------
function pickActionInfo(params) {
  const info = {};
  for (const k of ['coordinate', 'start_coordinate', 'text', 'scroll_direction', 'ref']) if (params[k] !== undefined) info[k] = params[k];
  return info;
}

async function recordGifFrame(tabId, action, screenshotResult) {
  if (!gif.isRecording()) return;
  try {
    const shot = screenshotResult
      ? {
          base64: screenshotResult.base64Image,
          imageWidth: screenshotResult.imageWidth,
          imageHeight: screenshotResult.imageHeight,
          frameWidth: screenshotResult.frameWidth,
          frameHeight: screenshotResult.frameHeight,
        }
      : await captureFrame(tabId, computerDeps);
    gif.addFrame({ ...shot, action });
  } catch {
    // a missed frame must never fail the action itself
  }
}

function dropFileInPage(base64, name, x, y, shownX, shownY) {
  const bin = atob(base64);
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
  const file = new File([new Blob([arr], { type: 'image/gif' })], name, { type: 'image/gif', lastModified: Date.now() });
  const dt = new DataTransfer();
  dt.items.add(file);
  const el = document.elementFromPoint(x, y);
  if (!el) throw new Error(`No element found at coordinates (${shownX}, ${shownY})`);
  for (const type of ['dragenter', 'dragover', 'drop']) {
    el.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt, clientX: x, clientY: y }));
  }
  return { output: `Successfully dropped ${name} (${Math.round(file.size / 1024)}KB) at (${shownX}, ${shownY})` };
}

async function gifAction(params) {
  switch (params.action) {
    case 'start_recording':
      return gif.startRecording();
    case 'stop_recording':
      return gif.stopRecording();
    case 'clear':
      return gif.clearAction();
    case 'export': {
      const download = params.download === true;
      const coordinate = Array.isArray(params.coordinate) && params.coordinate.length === 2 ? params.coordinate : null;
      if (!download && !coordinate) {
        throw new Error('coordinate parameter is required for export action (or set download: true to download the GIF)');
      }
      if (gif.frameCount() === 0) {
        throw new ToolError('gif_no_frames', "No frames recorded for this tab group. Use 'start_recording' and perform browser actions first.");
      }
      const built = await gif.buildGif(params.options || {});
      const stamp = new Date().toISOString().replace(/[:.]/g, '-');
      const filename = params.filename || `recording-${stamp}.gif`;
      let output;
      let downloadNote = '';
      if (download) {
        const downloadId = await chrome.downloads.download({ url: `data:image/gif;base64,${built.base64}`, filename, saveAs: false });
        // wait (briefly) for Chrome to finish or reject the download
        for (let waited = 0; waited < 5000; waited += 250) {
          const [item] = await chrome.downloads.search({ id: downloadId });
          if (item && item.state === 'interrupted') throw new Error(`Chrome could not save the GIF (${item.error || 'download interrupted'})`);
          if (item && item.state === 'complete') break;
          if (waited >= 4750 && item) downloadNote = ` [download state: ${item.state}, danger: ${item.danger}, paused: ${item.paused}, bytes: ${item.bytesReceived}/${item.totalBytes}]`;
          await new Promise((r) => setTimeout(r, 250));
        }
        output = `Successfully exported GIF with ${built.frames} frames. Downloaded "${filename}" (${Math.round(built.size / 1024)}KB). Dimensions: ${built.width}x${built.height}. Recording cleared.${downloadNote}`;
      } else {
        const [vx, vy] = frameToViewport(params.tabId, coordinate, 'gif_creator export');
        const results = await chrome.scripting.executeScript({
          target: { tabId: params.tabId },
          func: dropFileInPage,
          args: [built.base64, filename, vx, vy, Math.round(coordinate[0]), Math.round(coordinate[1])],
        });
        if (!results || !results[0] || !results[0].result) throw new Error('Failed to upload GIF to page');
        output = `Successfully exported GIF with ${built.frames} frames. ${results[0].result.output}. Dimensions: ${built.width}x${built.height}. Recording cleared.`;
      }
      gif.clearFrames();
      return { output };
    }
    default:
      throw new Error(`Unknown action: ${params.action}. Must be one of: start_recording, stop_recording, export, clear`);
  }
}

// A hidden (background) tab renders no frames, and Chrome only acknowledges a mouse event once a
// frame is produced. Send the event, and if it is not acknowledged quickly force one frame by
// taking a tiny screenshot, then wait for the acknowledgement (bounded, so a stuck page cannot
// hang the tool for the full RPC timeout).
async function sendMouseEvent(tabId, params) {
  const call = chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', params);
  let acked = false;
  const tracked = call.then(
    () => {
      acked = true;
    },
    (err) => {
      acked = true;
      throw err;
    }
  );
  tracked.catch(() => {});
  await Promise.race([tracked.catch(() => {}), sleep(60)]);
  if (!acked) {
    chrome.debugger
      .sendCommand({ tabId }, 'Page.captureScreenshot', { format: 'jpeg', quality: 1, captureBeyondViewport: false, fromSurface: true })
      .catch(() => {});
    await Promise.race([tracked.catch(() => {}), sleep(3000)]);
  }
  if (acked) await call; // surface CDP errors
}

// Dependencies handed to lib/computer.js
const computerDeps = {
  async sendCommand(tabId, method, params) {
    if (!(await ensureDebuggerAttached(tabId))) {
      throw new ToolError(
        'debugger_attach_failed',
        `Could not attach the debugger to tab ${tabId}. Close DevTools on that tab (or any other debugger) and try again.`
      );
    }
    if (method === 'Input.dispatchMouseEvent') return sendMouseEvent(tabId, params);
    return chrome.debugger.sendCommand({ tabId }, method, params);
  },
  async groupTabIds() {
    await loadMcpGroupId();
    if (agentGroupId === null) return [];
    const tabs = await chrome.tabs.query({ groupId: agentGroupId });
    return tabs.map((t) => t.id).filter((id) => id !== undefined);
  },
};

const AGENT_UI_LABEL = 'klik is working here';
let agentStopped = false;

// Main RPC Request Dispatcher: enforces tab-group confinement for page.* methods and
// appends tabContext (like Claude in Chrome) to successful page.* results.
async function handleRequest(msg) {
  const { id, method, params = {} } = msg;
  const isPageMethod = typeof method === 'string' && method.startsWith('page.');
  try {
    if (isPageMethod) {
      if (typeof params.tabId !== 'number') {
        throw new ToolError('missing_tab_id', 'tabId (number) is required. Call tabs_context_mcp first to get valid tab IDs.');
      }
      await assertTabInMcpGroup(params.tabId);
      if (agentStopped) {
        agentStopped = false;
        throw new ToolError(
          'stopped_by_user',
          'The user pressed Stop in the page. Stop what you are doing, tell the user, and wait for their instructions.'
        );
      }
      ui(params.tabId, 'show', [AGENT_UI_LABEL]); // fire and forget
    }
    const res = await handleRequestInner(msg);
    if (isPageMethod && !res.error && res.result && typeof res.result === 'object' && !Array.isArray(res.result)) {
      const tabContext = await buildTabContext(params.tabId);
      if (tabContext) res.result.tabContext = tabContext;
    }
    return res;
  } catch (err) {
    return {
      type: 'res',
      id,
      error: { code: err.code || 'internal_error', message: err.message || String(err) },
    };
  }
}

async function handleRequestInner(msg) {
  const { id, method, params = {} } = msg;

  try {
    switch (method) {
      // 1. read_page: accessibility tree with stable element refs (ref_N)
      case 'page.readPage': {
        const tabId = params.tabId;
        const tab = await chrome.tabs.get(tabId);
        if (isRestrictedUrl(tab.url)) {
          throw new ToolError('restricted_page', `Cannot read restricted page: ${tab.url}`);
        }
        await injectPageTree(tabId);
        const results = await chrome.scripting.executeScript({
          target: { tabId },
          injectImmediately: true,
          func: (filter, depth, maxChars, refId) => {
            if (typeof window.__klikGenerateTree !== 'function') {
              throw new Error('Accessibility tree function not found. Please refresh the page.');
            }
            return window.__klikGenerateTree(filter, depth, maxChars, refId);
          },
          args: [
            params.filter || null,
            typeof params.depth === 'number' ? params.depth : null,
            typeof params.max_chars === 'number' ? params.max_chars : 50000,
            params.ref_id || null,
          ],
        });
        const tree = results && results[0] ? results[0].result : null;
        if (!tree) throw new ToolError('injected_script_failed', 'Page script returned empty result');
        if (tree.error) throw new ToolError('ref_stale', tree.error);
        return {
          type: 'res',
          id,
          result: { output: `${tree.pageContent}\n\nViewport: ${tree.viewport.width}x${tree.viewport.height}` },
        };
      }

      // 2. computer: Unified mouse, keyboard, and screen interaction
      case 'page.computer': {
        const tabId = params.tabId;
        const tab = await chrome.tabs.get(tabId);
        if (isRestrictedUrl(tab.url)) {
          throw new ToolError('restricted_page', `Cannot interact with restricted page: ${tab.url}`);
        }
        const res = await runComputerAction(tabId, params, computerDeps);
        await recordGifFrame(tabId, { type: params.action, ...pickActionInfo(params) }, params.action === 'screenshot' ? res : null);
        const { frameWidth, frameHeight, imageWidth, imageHeight, ...clean } = res;
        return { type: 'res', id, result: clean };
      }

      // 3. form_input: set a control addressed by ref_N
      case 'page.formInput': {
        const tabId = params.tabId;
        if (!params.ref) throw new ToolError('invalid_params', 'ref parameter is required');
        if (params.value === undefined || params.value === null) throw new ToolError('invalid_params', 'Value parameter is required');
        if (params.action_summary) {
          await Promise.race([ui(tabId, 'show', [summaryLabel(params, undefined)]), new Promise((r) => setTimeout(r, 400))]);
        }
        await injectPageTree(tabId);
        const results = await chrome.scripting.executeScript({
          target: { tabId },
          func: formInputInPage,
          args: [params.ref, params.value],
        });
        const res = results && results[0] ? results[0].result : null;
        if (!res) {
          throw new ToolError(
            'injected_script_failed',
            'the page closed or navigated before the result came back, so the change may or may not have happened. Take a screenshot to check.'
          );
        }
        if (res.error) throw new ToolError(res.errorCode || 'input_invalid', res.error);
        return { type: 'res', id, result: { output: res.output } };
      }

      // 4. navigate: URL, back/forward, with "Leave site?" handling
      case 'page.navigate': {
        const tabId = params.tabId;
        const res = await navigateTab(tabId, params, { ensureAttached: ensureDebuggerAttached });
        await recordGifFrame(tabId, { type: 'navigate', url: params.url }, null);
        return { type: 'res', id, result: res };
      }

      // 5. find: natural-language element search (local matcher, no LLM)
      case 'page.find': {
        const tabId = params.tabId;
        const query = typeof params.query === 'string' ? params.query.replace(/\s+/g, ' ').trim() : '';
        if (!query) throw new ToolError('invalid_params', 'Query parameter is required');
        const tab = await chrome.tabs.get(tabId);
        if (isRestrictedUrl(tab.url)) throw new ToolError('restricted_page', `Cannot read restricted page: ${tab.url}`);
        await injectPageTree(tabId);
        const results = await chrome.scripting.executeScript({
          target: { tabId },
          injectImmediately: true,
          func: () => {
            if (typeof window.__klikGenerateTree !== 'function') {
              throw new Error('Accessibility tree function not found. Please refresh the page.');
            }
            return window.__klikGenerateTree('all', null, null, null, { findData: true });
          },
        });
        const data = results && results[0] ? results[0].result : null;
        if (!data) throw new ToolError('find_exception', 'Page script returned empty result');
        const found = findLocal(data, query);
        if (found.noMatch) throw new ToolError('find_no_match', found.noMatch);
        return { type: 'res', id, result: { output: found.output } };
      }

      // get_page_text: plain text of the main content
      case 'page.getPageText': {
        const tabId = params.tabId;
        const tab = await chrome.tabs.get(tabId);
        if (isRestrictedUrl(tab.url)) throw new ToolError('restricted_page', `Cannot read restricted page: ${tab.url}`);
        const maxChars = typeof params.max_chars === 'number' && params.max_chars > 0 ? params.max_chars : 50000;
        const results = await chrome.scripting.executeScript({ target: { tabId }, func: extractPageText, args: [maxChars] });
        const res = results && results[0] ? results[0].result : null;
        if (!res) {
          throw new ToolError('get_page_text_no_content', 'No main text content found. The content might be visual content only, or rendered in a canvas element.');
        }
        if (res.error) throw new ToolError('get_page_text_no_content', res.error);
        const note = res.fellBack
          ? " (the page's main content element had almost no text, so this is all of <body>. If what you expect is missing, the page may still be loading, or show it in a frame, canvas or video.)"
          : '';
        return {
          type: 'res',
          id,
          result: { output: `Title: ${res.title}\nURL: ${res.url}\nSource element: <${res.source}>${note}\n---\n${res.text}` },
        };
      }

      // read_network_requests / read_console_messages
      case 'page.networkRequests': {
        const tabId = params.tabId;
        await ensureDebuggerAttached(tabId);
        if (!enableNetworkTracking(tabId)) await rawSend(tabId, 'Network.enable', {}).catch(() => {});
        const limit = typeof params.limit === 'number' && params.limit > 0 ? params.limit : 100;
        const all = getNetworkRequests(tabId);
        const list = params.urlPattern ? all.filter((r) => r.url.includes(params.urlPattern)) : all;
        const output = formatNetwork(list, { urlPattern: params.urlPattern, limit });
        if (params.clear === true) clearNetwork(tabId);
        return { type: 'res', id, result: { output } };
      }

      case 'page.consoleMessages': {
        const tabId = params.tabId;
        await ensureDebuggerAttached(tabId);
        const limit = typeof params.limit === 'number' && params.limit > 0 ? params.limit : 100;
        const onlyErrors = params.onlyErrors === true;
        let messages;
        try {
          messages = getConsoleMessages(tabId, onlyErrors, params.pattern);
        } catch (err) {
          throw new ToolError('invalid_params', `Failed to read console messages: ${err.message}`);
        }
        const output = formatConsole(messages, { onlyErrors, limit });
        if (params.clear === true) clearConsole(tabId);
        return { type: 'res', id, result: { output } };
      }

      case 'page.javascript': {
        const res = await runJavascript(params.tabId, params, computerDeps);
        return { type: 'res', id, result: res };
      }

      case 'page.resizeWindow': {
        const res = await resizeWindow(params.tabId, params);
        return { type: 'res', id, result: res };
      }

      case 'page.fileUpload': {
        await injectPageTree(params.tabId);
        const res = await fileUpload(params.tabId, params);
        return { type: 'res', id, result: res };
      }

      case 'page.uploadImage': {
        await injectPageTree(params.tabId);
        const res = await uploadImage(params.tabId, params);
        return { type: 'res', id, result: res };
      }

      case 'page.gif': {
        try {
          const res = await gifAction(params);
          return { type: 'res', id, result: res };
        } catch (err) {
          if (err instanceof ToolError) throw err;
          throw new Error(`Failed to execute gif_creator: ${err instanceof Error ? err.message : 'Unknown error'}`);
        }
      }

      // 9. tabs_context_mcp: MCP tab group context (optionally creating the group)
      case 'tabs.context': {
        const ctx = await getOrCreateMcpTabContext({ createIfEmpty: !!params.createIfEmpty });
        if (!ctx) return { type: 'res', id, result: { exists: false } };
        return { type: 'res', id, result: { exists: true, ...ctx } };
      }

      // tabs.list: raw tab lookup used by the daemon for liveness checks
      case 'tabs.list': {
        await loadMcpGroupId();
        const ids = Array.isArray(params.tabIds) ? params.tabIds : null;
        const all = ids ? (await Promise.all(ids.map((i) => chrome.tabs.get(i).catch(() => null)))).filter(Boolean) : await chrome.tabs.query({});
        const tabs = all.map((t) => ({
          ...toTabSummary(t),
          active: t.active,
          groupId: t.groupId !== -1 ? t.groupId : null,
          inMcpGroup: agentGroupId !== null && t.groupId === agentGroupId,
        }));
        return { type: 'res', id, result: { tabs, tabGroupId: agentGroupId } };
      }

      // 10. tabs_create_mcp: new empty tab inside the MCP group
      case 'tabs.create': {
        await loadMcpGroupId();
        if (agentGroupId === null) {
          throw new ToolError(
            'tab_create_no_group',
            `No MCP tab group exists. ${NO_GROUP_NOTE} Use tabs_context_mcp with createIfEmpty: true first to create a group.`
          );
        }
        let windowId;
        try {
          ({ windowId } = await chrome.tabGroups.get(agentGroupId));
        } catch {
          agentGroupId = null;
          throw new ToolError(
            'tab_create_group_gone',
            'The MCP tab group no longer exists. Its tabs were closed or moved out of the group, and a moved tab may still be open. Call tabs_context_mcp with createIfEmpty: true to create a new group.'
          );
        }
        const tab = await chrome.tabs.create({ windowId, url: 'chrome://newtab', active: false });
        if (!tab.id) throw new ToolError('tab_create_exception', 'Failed to create tab - no tab ID returned');
        await chrome.tabs.group({ tabIds: tab.id, groupId: agentGroupId });
        const availableTabs = await listGroupTabs(agentGroupId);
        return {
          type: 'res',
          id,
          result: { tabId: tab.id, tabGroupId: agentGroupId, availableTabs },
        };
      }

      // 11. tabs_close_mcp: close a tab inside the MCP group
      case 'tabs.close': {
        const tabId = params.tabId;
        if (typeof tabId !== 'number' || !Number.isInteger(tabId)) {
          throw new ToolError('tab_close_invalid_params', 'tabId must be an integer');
        }
        await loadMcpGroupId();
        if (agentGroupId === null) {
          throw new ToolError(
            'tab_close_no_group',
            `No MCP tab group exists. ${NO_GROUP_NOTE} This call closed nothing. Call tabs_context_mcp with createIfEmpty: true if you need a working tab.`
          );
        }
        let tab;
        try {
          tab = await chrome.tabs.get(tabId);
        } catch {
          throw new ToolError('tab_close_not_found', `Tab ${tabId} does not exist (may have already been closed). Call tabs_context_mcp to see current tabs.`);
        }
        if (tab.groupId !== agentGroupId) {
          throw new ToolError(
            'tab_close_not_in_group',
            `Tab ${tabId} is not in this session's tab group. Only tabs visible to this session can be closed. Call tabs_context_mcp to see closable tabs.`
          );
        }
        await chrome.tabs.remove(tabId);
        return { type: 'res', id, result: { closed: true, tabId } };
      }

      case 'ext.reload': {
        setTimeout(() => { chrome.runtime.reload(); }, 50);
        return { type: 'res', id, result: { reloaded: true } };
      }

      default:
        return {
          type: 'res',
          id,
          error: { code: 'method_not_found', message: `Method '${method}' not recognized` },
        };
    }
  } catch (err) {
    return {
      type: 'res',
      id,
      error: { code: err.code || 'internal_error', message: err.message || String(err) },
    };
  }
}

// Connect to daemon WebSocket
async function connect() {
  if (
    ws &&
    (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)
  ) {
    return;
  }

  const config = await getConfig();
  if (!config.label || !config.token) {
    setBadge('!', '#e74c3c');
    return;
  }

  try {
    ws = new WebSocket(config.daemonUrl);
  } catch (err) {
    console.error('WebSocket creation error:', err);
    scheduleReconnect();
    return;
  }

  ws.onopen = () => {
    const hello = {
      type: 'hello',
      label: config.label,
      profile: config.label,
      instanceId: config.instanceId,
      token: config.token,
      version: (chrome.runtime && chrome.runtime.getManifest) ? chrome.runtime.getManifest().version : '2.0.1',
      protocol: 1,
    };
    ws.send(JSON.stringify(hello));
  };

  ws.onmessage = async (event) => {
    let msg;
    try {
      msg = JSON.parse(event.data);
    } catch {
      return;
    }

    if (msg.type === 'hello_ok') {
      setBadge('', '');
      reconnectDelay = 1000;
      startPing();
      return;
    }

    if (msg.type === 'hello_error') {
      setBadge('!', '#d93025');
      stopPing();
      const code = msg.code || msg.error;
      if (code === 'bad_token' || code === 'bad_label' || code === 'label_in_use' || code === 'bad_protocol') {
        return;
      }
      scheduleReconnect();
      return;
    }

    if (msg.type === 'pong') {
      return;
    }

    if (msg.id !== undefined && msg.method) {
      const res = await handleRequest(msg);
      if (ws && ws.readyState === WebSocket.OPEN) {
        if (!res.type) res.type = 'res';
        ws.send(JSON.stringify(res));
      }
    }
  };

  ws.onclose = () => {
    stopPing();
    setBadge('', '');
    scheduleReconnect();
  };

  ws.onerror = () => {
    try {
      ws.close();
    } catch {}
  };
}

function startPing() {
  stopPing();
  pingInterval = setInterval(() => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'ping' }));
    }
  }, 20000);
}

function stopPing() {
  if (pingInterval) {
    clearInterval(pingInterval);
    pingInterval = null;
  }
}

function scheduleReconnect() {
  if (reconnectTimeout) return;
  reconnectTimeout = setTimeout(() => {
    reconnectTimeout = null;
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY);
    connect();
  }, reconnectDelay);
}

// Keep-alive using alarms
chrome.alarms.create('keepAlive', { periodInMinutes: 0.5 });
chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === 'keepAlive') {
    if (!ws || ws.readyState === WebSocket.CLOSED) {
      connect();
    } else if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'ping' }));
    }
  }
});

// Runtime messages (e.g. from Stop HUD button)
chrome.runtime.onMessage.addListener((req, sender, sendResponse) => {
  if (req.type === 'EMERGENCY_STOP') {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({
        type: 'event',
        event: 'agent_stopped',
        reason: 'User pressed Emergency Stop button',
      }));
    }
    sendResponse({ status: 'stopped' });
  } else if (req.type === 'reconnect') {
    if (ws) {
      try {
        ws.close();
      } catch {}
    }
    reconnectDelay = 1000;
    connect();
    sendResponse({ status: 'ok' });
  } else if (req.type === 'EMERGENCY_STOP') {
    agentStopped = true;
    sendResponse({ status: 'ok' });
  } else if (req.type === 'getStatus') {
    (async () => {
      let activeTabIds = [];
      try {
        await loadMcpGroupId();
        if (agentGroupId !== null) {
          activeTabIds = (await listGroupTabs(agentGroupId)).map((t) => t.tabId);
        }
      } catch {}
      sendResponse({
        connected: ws && ws.readyState === WebSocket.OPEN,
        activeTabIds,
        groupId: agentGroupId,
      });
    })();
  }
  return true;
});

// Startup
connect();
