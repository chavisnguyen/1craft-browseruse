import { generateAccessibilityTreeFn } from './lib/accessibility_tree.js';
import { findElementInPage, findElementsQueryInPage, waitForElementInPage } from './lib/find_element.js';
import {
  setAgentActiveState,
  animateCursorTo,
  hideAgentIndicatorsForScreenshot,
  showAgentIndicatorsAfterScreenshot,
} from './lib/cursor.js';

let ws = null;
let pingInterval = null;
let reconnectTimeout = null;
let reconnectDelay = 1000;
const MAX_RECONNECT_DELAY = 30000;

let agentWindowId = null;
let agentGroupId = null;
const agentTabIds = new Set();
const attachedDebuggers = new Set();
const consoleLogsByTab = new Map();
const networkRequestsByTab = new Map();

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
  let windowMode = data.windowMode || 'dedicated';

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

async function getOrCreateWindow(windowMode) {
  if (windowMode !== 'dedicated') {
    return undefined;
  }

  if (agentWindowId !== null) {
    try {
      const win = await chrome.windows.get(agentWindowId);
      if (win) return win.id;
    } catch {
      agentWindowId = null;
    }
  }

  const win = await chrome.windows.create({
    focused: false,
    width: 1280,
    height: 900,
  });
  agentWindowId = win.id;
  return agentWindowId;
}

// Manages Chrome Tab Groups for agent tabs
async function ensureAgentTabGroup(tabId) {
  if (!chrome.tabGroups) return null;
  try {
    if (agentGroupId !== null) {
      try {
        const grp = await chrome.tabGroups.get(agentGroupId);
        if (grp) {
          await chrome.tabs.group({ tabIds: [tabId], groupId: agentGroupId });
          return agentGroupId;
        }
      } catch {
        agentGroupId = null;
      }
    }

    agentGroupId = await chrome.tabs.group({ tabIds: [tabId] });
    await chrome.tabGroups.update(agentGroupId, {
      title: 'OneCraft Agent',
      color: 'orange',
    });
    return agentGroupId;
  } catch (err) {
    console.warn('Tab group creation error:', err);
    return null;
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
      await chrome.debugger.sendCommand({ tabId }, 'Network.enable');
    } catch {}
    return true;
  } catch (err) {
    console.warn(`Debugger attach failed on tab ${tabId}:`, err);
    return false;
  }
}

chrome.debugger.onEvent.addListener(async (source, method, params) => {
  if (!source || !source.tabId) return;
  const tabId = source.tabId;
  const timeStr = new Date().toLocaleTimeString();

  // 1. Prevent CDP hang on window.alert / confirm / prompt
  if (method === 'Page.javascriptDialogOpening') {
    try {
      await chrome.debugger.sendCommand({ tabId }, 'Page.handleJavaScriptDialog', {
        accept: true,
        promptText: '',
      });
    } catch {}
    return;
  }

  // 2. Capture Console logs
  if (method === 'Runtime.consoleAPICalled') {
    if (!consoleLogsByTab.has(tabId)) consoleLogsByTab.set(tabId, []);
    const logs = consoleLogsByTab.get(tabId);
    const type = params.type || 'log';
    const text = (params.args || [])
      .map((a) => (a.value !== undefined ? String(a.value) : (a.description || '')))
      .join(' ');
    logs.push(`[${timeStr}] [${type}] ${text}`);
    if (logs.length > 500) logs.shift();
  } else if (method === 'Runtime.exceptionThrown') {
    if (!consoleLogsByTab.has(tabId)) consoleLogsByTab.set(tabId, []);
    const logs = consoleLogsByTab.get(tabId);
    const desc = params.exceptionDetails
      ? (params.exceptionDetails.exception ? params.exceptionDetails.exception.description : params.exceptionDetails.text)
      : 'Unknown exception';
    logs.push(`[${timeStr}] [error] ${desc}`);
    if (logs.length > 500) logs.shift();
  }

  // 3. Capture Network requests and responses
  if (method === 'Network.requestWillBeSent') {
    if (!networkRequestsByTab.has(tabId)) networkRequestsByTab.set(tabId, []);
    const reqs = networkRequestsByTab.get(tabId);
    reqs.push({
      requestId: params.requestId,
      url: params.request.url,
      method: params.request.method,
      type: params.type || 'Other',
      timestamp: Date.now(),
      status: null,
      mimeType: null,
    });
    if (reqs.length > 200) reqs.shift();
  } else if (method === 'Network.responseReceived') {
    const reqs = networkRequestsByTab.get(tabId);
    if (reqs) {
      const match = reqs.find((r) => r.requestId === params.requestId);
      if (match) {
        match.status = params.response.status;
        match.mimeType = params.response.mimeType;
      }
    }
  }
});

chrome.debugger.onDetach.addListener((source) => {
  if (source.tabId) {
    attachedDebuggers.delete(source.tabId);
  }
});

chrome.tabs.onRemoved.addListener((tabId) => {
  agentTabIds.delete(tabId);
  consoleLogsByTab.delete(tabId);
  networkRequestsByTab.delete(tabId);
  if (attachedDebuggers.has(tabId)) {
    try {
      chrome.debugger.detach({ tabId });
    } catch {}
    attachedDebuggers.delete(tabId);
  }
});

chrome.windows.onRemoved.addListener((windowId) => {
  if (agentWindowId === windowId) {
    agentWindowId = null;
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

function getKeyDetails(keyStr) {
  switch ((keyStr || '').toLowerCase()) {
    case 'enter':
      return { key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13, text: '\r' };
    case 'tab':
      return { key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 };
    case 'escape':
    case 'esc':
      return { key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 };
    case 'backspace':
      return { key: 'Backspace', code: 'Backspace', windowsVirtualKeyCode: 8 };
    case 'arrowdown':
    case 'down':
      return { key: 'ArrowDown', code: 'ArrowDown', windowsVirtualKeyCode: 40 };
    case 'arrowup':
    case 'up':
      return { key: 'ArrowUp', code: 'ArrowUp', windowsVirtualKeyCode: 38 };
    case 'arrowleft':
    case 'left':
      return { key: 'ArrowLeft', code: 'ArrowLeft', windowsVirtualKeyCode: 37 };
    case 'arrowright':
    case 'right':
      return { key: 'ArrowRight', code: 'ArrowRight', windowsVirtualKeyCode: 39 };
    default:
      return { key: keyStr || 'Enter', code: keyStr || 'Enter', windowsVirtualKeyCode: 13, text: keyStr ? keyStr : '\r' };
  }
}

// Single Computer Action Dispatcher
async function dispatchComputerAction(tabId, params) {
  const action = (params.action || 'left_click').toLowerCase();
  const selector = params.selector || '';
  const coordinate = params.coordinate || null;

  let x = coordinate && coordinate[0] !== undefined ? coordinate[0] : null;
  let y = coordinate && coordinate[1] !== undefined ? coordinate[1] : null;

  // Resolve target coordinates if selector provided
  let findRes = null;
  if (selector && (x === null || y === null)) {
    const results = await chrome.scripting.executeScript({
      target: { tabId },
      func: findElementInPage,
      args: [selector, 'prepare_click', ''],
    });
    findRes = results && results[0] ? results[0].result : null;
    if (!findRes || !findRes.found) {
      throw new Error((findRes && findRes.error) || `element not found: no element matching '${selector}'`);
    }
    x = findRes.center.x;
    y = findRes.center.y;
  }

  // 1. Mouse move / hover
  if (action === 'mouse_move' || action === 'hover') {
    if (x !== null && y !== null) {
      await chrome.scripting.executeScript({
        target: { tabId },
        func: animateCursorTo,
        args: [x, y, false, `Hovering ${selector || ''}`.trim()],
      }).catch(() => {});

      const hasDebugger = await ensureDebuggerAttached(tabId);
      if (hasDebugger) {
        await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
          type: 'mouseMoved',
          x,
          y,
        }).catch(() => {});
      }
    }
    return { action, hovered: true, coordinate: [x, y] };
  }

  // 2. Mouse clicks (left, right, middle, double, triple)
  if (
    action === 'left_click' ||
    action === 'click' ||
    action === 'right_click' ||
    action === 'middle_click' ||
    action === 'double_click' ||
    action === 'triple_click'
  ) {
    const button = (action === 'right_click') ? 'right' : (action === 'middle_click') ? 'middle' : 'left';
    const clickCount = (action === 'double_click') ? 2 : (action === 'triple_click') ? 3 : 1;

    if (x !== null && y !== null) {
      await chrome.scripting.executeScript({
        target: { tabId },
        func: animateCursorTo,
        args: [x, y, true, `Clicking ${selector || ''}`.trim()],
      }).catch(() => {});

      const hasDebugger = await ensureDebuggerAttached(tabId);
      if (hasDebugger) {
        await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
          type: 'mouseMoved',
          x,
          y,
        }).catch(() => {});

        for (let i = 1; i <= clickCount; i++) {
          await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
            type: 'mousePressed',
            x,
            y,
            button,
            clickCount: i,
          }).catch(() => {});
          await sleep(25);
          await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
            type: 'mouseReleased',
            x,
            y,
            button,
            clickCount: i,
          }).catch(() => {});
          if (i < clickCount) await sleep(60);
        }
      }

      // Hybrid Universal Click Guarantee for left click
      if (button === 'left' && selector) {
        await chrome.scripting.executeScript({
          target: { tabId },
          func: findElementInPage,
          args: [selector, 'ensure_click', ''],
        }).catch(() => {});
      }
    }
    await sleep(150);
    return { action, clicked: true, button, clickCount, coordinate: [x, y] };
  }

  // 3. Typing text
  if (action === 'type') {
    const text = params.text || '';
    if (selector) {
      await chrome.scripting.executeScript({
        target: { tabId },
        func: findElementInPage,
        args: [selector, 'prepare_type', ''],
      });
      if (findRes && findRes.center) {
        await chrome.scripting.executeScript({
          target: { tabId },
          func: animateCursorTo,
          args: [findRes.center.x, findRes.center.y, false, `Typing into ${selector}`],
        }).catch(() => {});
      }
    }

    const hasDebugger = await ensureDebuggerAttached(tabId);
    let typed = false;
    if (hasDebugger) {
      try {
        await chrome.debugger.sendCommand({ tabId }, 'Input.insertText', { text });
        typed = true;
      } catch {}
    }
    if (!typed && selector) {
      await chrome.scripting.executeScript({
        target: { tabId },
        func: findElementInPage,
        args: [selector, 'type_fallback', text],
      });
    }
    return { action, typed: true, text };
  }

  // 4. Key press
  if (action === 'key' || action === 'press_key') {
    const key = params.key || 'Enter';
    const kd = getKeyDetails(key);
    const hasDebugger = await ensureDebuggerAttached(tabId);
    let pressed = false;

    if (hasDebugger) {
      try {
        await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchKeyEvent', {
          type: 'rawKeyDown',
          key: kd.key,
          code: kd.code,
          windowsVirtualKeyCode: kd.windowsVirtualKeyCode,
        });
        if (kd.text) {
          await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchKeyEvent', {
            type: 'char',
            text: kd.text,
          });
        }
        await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchKeyEvent', {
          type: 'keyUp',
          key: kd.key,
          code: kd.code,
          windowsVirtualKeyCode: kd.windowsVirtualKeyCode,
        });
        pressed = true;
      } catch {}
    }

    if (!pressed) {
      await chrome.scripting.executeScript({
        target: { tabId },
        func: (kName, kCode) => {
          const target = document.activeElement || document.body;
          target.dispatchEvent(new KeyboardEvent('keydown', { key: kName, code: kCode, keyCode: 13, which: 13, bubbles: true }));
          target.dispatchEvent(new KeyboardEvent('keyup', { key: kName, code: kCode, keyCode: 13, which: 13, bubbles: true }));
          if (kName === 'Enter' && target && target.form) {
            if (target.form.requestSubmit) target.form.requestSubmit();
            else target.form.submit();
          }
        },
        args: [kd.key, kd.code],
      });
    }
    return { action, pressed: true, key };
  }

  // 5. Scroll
  if (action === 'scroll') {
    const direction = (params.direction || 'down').toLowerCase();
    const amount = params.amount > 0 ? params.amount : 500;

    await chrome.scripting.executeScript({
      target: { tabId },
      func: (dir, amt) => {
        if (dir === 'up') window.scrollBy({ top: -amt, behavior: 'instant' });
        else if (dir === 'top') window.scrollTo({ top: 0, behavior: 'instant' });
        else if (dir === 'bottom') window.scrollTo({ top: document.body.scrollHeight, behavior: 'instant' });
        else window.scrollBy({ top: amt, behavior: 'instant' });
      },
      args: [direction, amount],
    });
    await sleep(200);
    return { action, scrolled: true, direction, amount };
  }

  // 6. Drag and Drop
  if (action === 'drag' || action === 'drag_and_drop') {
    const fromSel = params.from || selector;
    const toSel = params.to;
    if (!fromSel || !toSel) throw new Error('drag requires both "from" and "to" selectors');

    const fromRes = (await chrome.scripting.executeScript({ target: { tabId }, func: findElementInPage, args: [fromSel, 'find', ''] }))[0].result;
    const toRes = (await chrome.scripting.executeScript({ target: { tabId }, func: findElementInPage, args: [toSel, 'find', ''] }))[0].result;
    if (!fromRes || !fromRes.found) throw new Error(`Source element not found: '${fromSel}'`);
    if (!toRes || !toRes.found) throw new Error(`Target element not found: '${toSel}'`);

    const { x: fx, y: fy } = fromRes.center;
    const { x: tx, y: ty } = toRes.center;

    await chrome.scripting.executeScript({ target: { tabId }, func: animateCursorTo, args: [fx, fy, true, 'Dragging element...'] }).catch(() => {});

    const hasDebugger = await ensureDebuggerAttached(tabId);
    if (hasDebugger) {
      await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', { type: 'mousePressed', x: fx, y: fy, button: 'left', clickCount: 1 });
      const steps = 6;
      for (let i = 1; i <= steps; i++) {
        const mx = Math.round(fx + (tx - fx) * (i / steps));
        const my = Math.round(fy + (ty - fy) * (i / steps));
        await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', { type: 'mouseMoved', x: mx, y: my, button: 'left' });
        await sleep(30);
      }
      await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', { type: 'mouseReleased', x: tx, y: ty, button: 'left', clickCount: 1 });
    }

    await chrome.scripting.executeScript({ target: { tabId }, func: animateCursorTo, args: [tx, ty, false, 'Dropped element'] }).catch(() => {});
    return { action, dragged: true, from: fromSel, to: toSel };
  }

  // 7. Screenshot with visual indicator suppression
  if (action === 'screenshot') {
    await chrome.scripting.executeScript({
      target: { tabId },
      func: hideAgentIndicatorsForScreenshot,
    }).catch(() => {});

    let base64Png = '';
    try {
      const hasDebugger = await ensureDebuggerAttached(tabId);
      if (hasDebugger) {
        try {
          const shot = await chrome.debugger.sendCommand({ tabId }, 'Page.captureScreenshot', { format: 'png' });
          base64Png = shot.data;
        } catch {}
      }
      if (!base64Png) {
        const tab = await chrome.tabs.get(tabId);
        const dataUrl = await chrome.tabs.captureVisibleTab(tab.windowId, { format: 'png' });
        base64Png = dataUrl.replace(/^data:image\/png;base64,/, '');
      }
    } finally {
      await chrome.scripting.executeScript({
        target: { tabId },
        func: showAgentIndicatorsAfterScreenshot,
      }).catch(() => {});
    }

    return { action, screenshot: true, pngBase64: base64Png };
  }

  throw new Error(`Unsupported computer action: '${action}'`);
}

// Main RPC Request Dispatcher
async function handleRequest(msg) {
  const { id, method, params = {} } = msg;

  try {
    switch (method) {
      // 1. read_page: Returns full ARIA Accessibility Tree + Deterministic Refs
      case 'page.readPage': {
        const tabId = params.tabId;
        const tab = await chrome.tabs.get(tabId);
        if (!tab) return { type: 'res', id, error: { code: 'tab_not_found', message: `Tab ${tabId} not found` } };
        if (isRestrictedUrl(tab.url)) {
          return { type: 'res', id, error: { code: 'restricted_page', message: `Cannot read restricted page: ${tab.url}` } };
        }

        // Activate glowing border & status
        await chrome.scripting.executeScript({
          target: { tabId },
          func: setAgentActiveState,
          args: [true, 'Reading page...'],
        }).catch(() => {});

        const results = await chrome.scripting.executeScript({
          target: { tabId },
          func: generateAccessibilityTreeFn,
          args: [params.filter || 'interactive'],
        });

        const data = results && results[0] ? results[0].result : { elements: '', url: tab.url, title: tab.title };
        return {
          type: 'res',
          id,
          result: data,
        };
      }

      // 2. computer: Unified mouse, keyboard, and screen interaction
      case 'page.computer': {
        const tabId = params.tabId;
        const tab = await chrome.tabs.get(tabId);
        if (!tab) return { type: 'res', id, error: { code: 'tab_not_found', message: `Tab ${tabId} not found` } };
        if (isRestrictedUrl(tab.url)) {
          return { type: 'res', id, error: { code: 'restricted_page', message: `Cannot interact with restricted page: ${tab.url}` } };
        }

        const res = await dispatchComputerAction(tabId, params);
        return { type: 'res', id, result: res };
      }

      // 3. form_input: Smart input filling, checkbox toggling, select dropdowns
      case 'page.formInput': {
        const tabId = params.tabId;
        const refId = String(params.refId || params.selector || '');
        const tab = await chrome.tabs.get(tabId);
        if (!tab) return { type: 'res', id, error: { code: 'tab_not_found', message: `Tab ${tabId} not found` } };

        const formOpts = {};
        if (params.value !== undefined && params.value !== null) formOpts.value = String(params.value);
        if (params.checked !== undefined && params.checked !== null) formOpts.checked = Boolean(params.checked);

        const results = await chrome.scripting.executeScript({
          target: { tabId },
          func: findElementInPage,
          args: [refId, 'form_input', formOpts],
        });

        const findRes = results && results[0] ? results[0].result : null;
        if (!findRes || !findRes.found) {
          return { type: 'res', id, error: { code: 'element_not_found', message: `Form element not found: '${refId}'` } };
        }

        if (findRes.center) {
          await chrome.scripting.executeScript({
            target: { tabId },
            func: animateCursorTo,
            args: [findRes.center.x, findRes.center.y, false, `Setting form input ${refId}`],
          }).catch(() => {});
        }

        return { type: 'res', id, result: findRes };
      }

      // 4. navigate: URL navigation, Back, Forward, Reload
      case 'page.navigate': {
        const tabId = params.tabId;
        const action = (params.action || '').toLowerCase();
        const url = params.url || '';

        if (action === 'back') {
          await chrome.tabs.goBack(tabId);
        } else if (action === 'forward') {
          await chrome.tabs.goForward(tabId);
        } else if (action === 'reload') {
          await chrome.tabs.reload(tabId);
        } else if (url) {
          const loadPromise = waitForTabLoad(tabId, 25000);
          await chrome.tabs.update(tabId, { url });
          await loadPromise;
        }

        await sleep(150);
        const updated = await chrome.tabs.get(tabId);
        return {
          type: 'res',
          id,
          result: { tabId: updated.id, url: updated.url, title: updated.title },
        };
      }

      // 5. find: Semantic natural language element search
      case 'page.find': {
        const tabId = params.tabId;
        const query = params.query || '';
        const results = await chrome.scripting.executeScript({
          target: { tabId },
          func: findElementsQueryInPage,
          args: [query],
        });
        const matches = results && results[0] ? results[0].result : [];
        return { type: 'res', id, result: { query, matches } };
      }

      // 6. read_network_requests: Reads captured network traffic
      case 'page.readNetworkRequests': {
        const tabId = params.tabId;
        const clear = params.clear !== false;
        const filter = (params.filter || '').toLowerCase();
        const reqs = networkRequestsByTab.get(tabId) || [];
        let filtered = [...reqs];
        if (filter) {
          filtered = filtered.filter((r) => r.url.toLowerCase().includes(filter));
        }
        if (clear) {
          networkRequestsByTab.set(tabId, []);
        }
        return { type: 'res', id, result: { requests: filtered } };
      }

      // 7. read_console_messages: Reads captured console messages
      case 'page.readConsoleMessages': {
        const tabId = params.tabId;
        const clear = params.clear !== false;
        const logs = consoleLogsByTab.get(tabId) || [];
        const resultLogs = [...logs];
        if (clear) {
          consoleLogsByTab.set(tabId, []);
        }
        return { type: 'res', id, result: { logs: resultLogs } };
      }

      // 8. browser_batch: Sequentially executes multiple computer/navigation actions in 1 turn
      case 'page.browserBatch': {
        const tabId = params.tabId;
        const actions = Array.isArray(params.actions) ? params.actions : [];
        const results = [];
        for (const act of actions) {
          const res = await dispatchComputerAction(tabId, act);
          results.push(res);
        }
        return { type: 'res', id, result: { results } };
      }

      // 9. tabs_context_mcp: Gets tab list, group info, and active tab
      case 'tabs.context':
      case 'tabs.list': {
        const allTabs = await chrome.tabs.query({});
        const tabs = [];
        for (const t of allTabs) {
          if (agentTabIds.has(t.id)) {
            tabs.push({
              tabId: t.id,
              url: t.url || '',
              title: t.title || '',
              active: t.active,
              groupId: t.groupId !== -1 ? t.groupId : null,
            });
          }
        }
        return { type: 'res', id, result: { tabs, agentGroupId } };
      }

      // 10. tabs_create_mcp: Creates new managed tab & assigns to Tab Group
      case 'tabs.create': {
        const config = await getConfig();
        const windowId = await getOrCreateWindow(config.windowMode);
        const createProps = {
          url: params.url || 'about:blank',
          active: true,
        };
        if (windowId !== undefined) {
          createProps.windowId = windowId;
        }

        const tab = await chrome.tabs.create(createProps);
        agentTabIds.add(tab.id);
        await ensureAgentTabGroup(tab.id);

        if (params.url && params.url !== 'about:blank') {
          await waitForTabLoad(tab.id, 30000);
        }

        const updated = await chrome.tabs.get(tab.id);
        return {
          type: 'res',
          id,
          result: {
            tabId: updated.id,
            url: updated.url || '',
            title: updated.title || '',
            groupId: agentGroupId,
          },
        };
      }

      // 11. tabs_close_mcp: Closes a tab
      case 'tabs.close': {
        const tabId = params.tabId;
        try {
          await chrome.tabs.remove(tabId);
        } catch {}
        agentTabIds.delete(tabId);
        return { type: 'res', id, result: { closed: true, tabId } };
      }

      // 12. evaluate: Execute JavaScript in page
      case 'page.evaluate': {
        const tabId = params.tabId;
        const script = params.script || '';
        const results = await chrome.scripting.executeScript({
          target: { tabId },
          world: 'MAIN',
          func: (codeStr) => {
            try {
              return (0, eval)(codeStr);
            } catch (e) {
              return { error: e.message || String(e) };
            }
          },
          args: [script],
        });
        return {
          type: 'res',
          id,
          result: { value: results && results[0] ? results[0].result : null },
        };
      }

      // 13. upload_file: Upload file to input
      case 'page.uploadFile': {
        const tabId = params.tabId;
        const selector = params.selector;
        const files = params.files || [];
        const hasDebugger = await ensureDebuggerAttached(tabId);
        if (!hasDebugger) {
          return { type: 'res', id, error: { code: 'debugger_unavailable', message: 'Debugger required for file uploads' } };
        }
        const doc = await chrome.debugger.sendCommand({ tabId }, 'DOM.getDocument');
        const search = await chrome.debugger.sendCommand({ tabId }, 'DOM.querySelector', {
          nodeId: doc.root.nodeId,
          selector,
        });
        if (!search.nodeId) {
          return { type: 'res', id, error: { code: 'element_not_found', message: `File input element not found: '${selector}'` } };
        }
        await chrome.debugger.sendCommand({ tabId }, 'DOM.setFileInputFiles', {
          files,
          nodeId: search.nodeId,
        });
        return { type: 'res', id, result: { uploaded: true, files } };
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
      error: { code: 'internal_error', message: err.message || String(err) },
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
      version: (chrome.runtime && chrome.runtime.getManifest) ? chrome.runtime.getManifest().version : '2.0.0',
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
  } else if (req.type === 'getStatus') {
    sendResponse({
      connected: ws && ws.readyState === WebSocket.OPEN,
      activeTabIds: Array.from(agentTabIds),
      groupId: agentGroupId,
    });
  }
  return true;
});

// Startup
connect();
