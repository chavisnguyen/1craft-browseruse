import { snapshotFn } from './lib/snapshot.js';
import { findElementInPage } from './lib/find_element.js';

let ws = null;
let pingInterval = null;
let reconnectTimeout = null;
let reconnectDelay = 1000;
const MAX_RECONNECT_DELAY = 30000;

let agentWindowId = null;
const agentTabIds = new Set();
const attachedDebuggers = new Set();

// Helper to generate UUID v4
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

// Badge management
function setBadge(text, color) {
  chrome.action.setBadgeText({ text });
  if (color) {
    chrome.action.setBadgeBackgroundColor({ color });
  }
}

// Map key string to CDP Key details
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

// Configuration loader
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

  // Fallback to pre-generated default_config.json if token is missing
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
    } catch {
      // ignore if default_config.json doesn't exist
    }
  }

  // Auto-derive label from Chrome signed-in email if label is empty or default
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

// Dedicated window management
async function getOrCreateWindow(windowMode) {
  if (windowMode !== 'dedicated') {
    return undefined; // create tab in current/default window
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

// Debugger lifecycle management
async function ensureDebuggerAttached(tabId) {
  if (attachedDebuggers.has(tabId)) {
    return true;
  }
  try {
    await chrome.debugger.attach({ tabId }, '1.3');
    attachedDebuggers.add(tabId);
    return true;
  } catch (err) {
    console.warn(`Debugger attach failed on tab ${tabId}:`, err);
    return false;
  }
}

chrome.debugger.onDetach.addListener((source, reason) => {
  if (source.tabId) {
    attachedDebuggers.delete(source.tabId);
  }
});

// Clean up closed tabs from our sets
chrome.tabs.onRemoved.addListener((tabId) => {
  agentTabIds.delete(tabId);
  if (attachedDebuggers.has(tabId)) {
    try {
      chrome.debugger.detach({ tabId });
    } catch {
      // ignore
    }
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

// Helper to wait for a tab to finish loading
async function waitForTabLoad(tabId, timeoutMs = 30000) {
  try {
    const current = await chrome.tabs.get(tabId);
    if (current && current.status === 'complete') {
      return current;
    }
  } catch {
    // ignore
  }

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
      resolve(null); // resolve even on timeout so we don't reject abruptly
    }, timeoutMs);

    chrome.tabs.onUpdated.addListener(listener);
  });
}

// Sleep helper
function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

// Map key string to CDP Key details
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
    default:
      return { key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13, text: '\r' };
  }
}

// Request handlers
async function handleRequest(msg) {
  const { id, method, params = {} } = msg;

  try {
    switch (method) {
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
          },
        };
      }

      case 'tabs.list': {
        const requested =
          Array.isArray(params.tabIds) && params.tabIds.length > 0
            ? new Set(params.tabIds.map((tid) => Number(tid)))
            : null;

        const tabs = [];
        for (const tabId of Array.from(agentTabIds)) {
          if (requested && !requested.has(tabId)) {
            continue;
          }
          try {
            const t = await chrome.tabs.get(tabId);
            tabs.push({
              tabId: t.id,
              url: t.url || '',
              title: t.title || '',
            });
          } catch {
            agentTabIds.delete(tabId);
          }
        }
        return {
          type: 'res',
          id,
          result: { tabs },
        };
      }

      case 'tabs.close': {
        const tabId = params.tabId;
        try {
          await chrome.tabs.remove(tabId);
        } catch {
          // tab might already be closed
        }
        agentTabIds.delete(tabId);
        if (attachedDebuggers.has(tabId)) {
          try {
            chrome.debugger.detach({ tabId });
          } catch {
            // ignore
          }
          attachedDebuggers.delete(tabId);
        }
        return { type: 'res', id, result: {} };
      }

      case 'page.navigate': {
        const tabId = params.tabId;
        await chrome.tabs.update(tabId, { url: params.url });
        await waitForTabLoad(tabId, 30000);
        await sleep(1000); // 1s settling time matching rod backend
        return { type: 'res', id, result: {} };
      }

      case 'page.snapshot': {
        const tabId = params.tabId;
        const tab = await chrome.tabs.get(tabId);
        if (!tab) {
          return {
            type: 'res',
            id,
            error: { code: 'tab_not_found', message: `Tab ${tabId} not found` },
          };
        }

        if (isRestrictedUrl(tab.url)) {
          return {
            type: 'res',
            id,
            error: {
              code: 'restricted_page',
              message: `Cannot snapshot restricted page: ${tab.url}`,
            },
          };
        }

        const results = await chrome.scripting.executeScript({
          target: { tabId },
          func: snapshotFn,
        });

        const elements = results && results[0] ? results[0].result : '';
        return {
          type: 'res',
          id,
          result: {
            url: tab.url || '',
            title: tab.title || '',
            elements: elements || '',
          },
        };
      }

      case 'page.click': {
        const tabId = params.tabId;
        const selector = params.selector;
        const tab = await chrome.tabs.get(tabId);
        if (!tab) {
          return {
            type: 'res',
            id,
            error: { code: 'tab_not_found', message: `Tab ${tabId} not found` },
          };
        }

        if (isRestrictedUrl(tab.url)) {
          return {
            type: 'res',
            id,
            error: {
              code: 'restricted_page',
              message: `Cannot interact with restricted page: ${tab.url}`,
            },
          };
        }

        // Find element in page
        const results = await chrome.scripting.executeScript({
          target: { tabId },
          func: findElementInPage,
          args: [selector, 'find', ''],
        });

        const findRes = results && results[0] ? results[0].result : null;
        if (!findRes || !findRes.found) {
          return {
            type: 'res',
            id,
            error: {
              code: 'element_not_found',
              message: (findRes && findRes.error) || `element not found: no element matching '${selector}'`,
            },
          };
        }

        await sleep(200); // 200ms settling time after scrollIntoView

        // Try clicking via debugger
        const hasDebugger = await ensureDebuggerAttached(tabId);
        let clicked = false;
        if (hasDebugger) {
          try {
            const { x, y } = findRes.center;
            await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
              type: 'mouseMoved',
              x,
              y,
            });
            await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
              type: 'mousePressed',
              x,
              y,
              button: 'left',
              clickCount: 1,
            });
            await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
              type: 'mouseReleased',
              x,
              y,
              button: 'left',
              clickCount: 1,
            });
            clicked = true;
          } catch (e) {
            console.warn('Debugger click failed, trying fallback:', e);
          }
        }

        if (!clicked) {
          // Fallback to direct DOM el.click()
          await chrome.scripting.executeScript({
            target: { tabId },
            func: findElementInPage,
            args: [selector, 'click_fallback', ''],
          });
        }

        await sleep(1000); // 1s settling time matching rod backend
        return { type: 'res', id, result: {} };
      }

      case 'page.type': {
        const tabId = params.tabId;
        const selector = params.selector;
        const text = params.text || '';
        const tab = await chrome.tabs.get(tabId);
        if (!tab) {
          return {
            type: 'res',
            id,
            error: { code: 'tab_not_found', message: `Tab ${tabId} not found` },
          };
        }

        if (isRestrictedUrl(tab.url)) {
          return {
            type: 'res',
            id,
            error: {
              code: 'restricted_page',
              message: `Cannot interact with restricted page: ${tab.url}`,
            },
          };
        }

        // Prepare element: focus and select all existing text to overwrite
        const results = await chrome.scripting.executeScript({
          target: { tabId },
          func: findElementInPage,
          args: [selector, 'prepare_type', ''],
        });

        const findRes = results && results[0] ? results[0].result : null;
        if (!findRes || !findRes.found) {
          return {
            type: 'res',
            id,
            error: {
              code: 'element_not_found',
              message: (findRes && findRes.error) || `element not found: no element matching '${selector}'`,
            },
          };
        }

        // Try inserting text via debugger (replaces selection)
        const hasDebugger = await ensureDebuggerAttached(tabId);
        let typed = false;
        if (hasDebugger) {
          try {
            await chrome.debugger.sendCommand({ tabId }, 'Input.insertText', { text });
            typed = true;
          } catch (e) {
            console.warn('Debugger insertText failed, trying fallback:', e);
          }
        }

        if (!typed) {
          // Fallback: direct value assignment + event dispatching
          await chrome.scripting.executeScript({
            target: { tabId },
            func: findElementInPage,
            args: [selector, 'type_fallback', text],
          });
        }

        return { type: 'res', id, result: {} };
      }

      case 'page.pressKey': {
        const tabId = params.tabId;
        const key = params.key || 'Enter';
        const tab = await chrome.tabs.get(tabId);
        if (!tab) {
          return {
            type: 'res',
            id,
            error: { code: 'tab_not_found', message: `Tab ${tabId} not found` },
          };
        }

        if (isRestrictedUrl(tab.url)) {
          return {
            type: 'res',
            id,
            error: {
              code: 'restricted_page',
              message: `Cannot interact with restricted page: ${tab.url}`,
            },
          };
        }

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
          } catch (e) {
            console.warn('Debugger dispatchKeyEvent failed:', e);
          }
        }

        if (!pressed) {
          // Fallback via DOM KeyboardEvent dispatch on active element
          await chrome.scripting.executeScript({
            target: { tabId },
            func: (kName, kCode) => {
              const target = document.activeElement || document.body;
              target.dispatchEvent(new KeyboardEvent('keydown', { key: kName, code: kCode, bubbles: true }));
              target.dispatchEvent(new KeyboardEvent('keyup', { key: kName, code: kCode, bubbles: true }));
            },
            args: [kd.key, kd.code],
          });
        }

        await sleep(500); // 500ms settling time matching rod backend
        return { type: 'res', id, result: {} };
      }

      case 'page.screenshot': {
        const tabId = params.tabId;
        const tab = await chrome.tabs.get(tabId);
        if (!tab) {
          return {
            type: 'res',
            id,
            error: { code: 'tab_not_found', message: `Tab ${tabId} not found` },
          };
        }

        if (isRestrictedUrl(tab.url)) {
          return {
            type: 'res',
            id,
            error: {
              code: 'restricted_page',
              message: `Cannot capture restricted page: ${tab.url}`,
            },
          };
        }

        const hasDebugger = await ensureDebuggerAttached(tabId);
        let base64Png = '';

        if (hasDebugger) {
          try {
            const shot = await chrome.debugger.sendCommand({ tabId }, 'Page.captureScreenshot', {
              format: 'png',
            });
            base64Png = shot.data;
          } catch (e) {
            console.warn('Debugger captureScreenshot failed, trying fallback:', e);
          }
        }

        if (!base64Png) {
          // Fallback to captureVisibleTab
          try {
            const dataUrl = await chrome.tabs.captureVisibleTab(tab.windowId, { format: 'png' });
            base64Png = dataUrl.replace(/^data:image\/png;base64,/, '');
          } catch (e) {
            return {
              type: 'res',
              id,
              error: { code: 'debugger_unavailable', message: `Failed to capture screenshot: ${e.message}` },
            };
          }
        }

        return {
          type: 'res',
          id,
          result: { pngBase64: base64Png },
        };
      }

      case 'page.evaluate': {
        const tabId = params.tabId;
        const script = params.script || '';
        const tab = await chrome.tabs.get(tabId);
        if (!tab) {
          return {
            type: 'res',
            id,
            error: { code: 'tab_not_found', message: `Tab ${tabId} not found` },
          };
        }

        if (isRestrictedUrl(tab.url)) {
          return {
            type: 'res',
            id,
            error: {
              code: 'restricted_page',
              message: `Cannot evaluate on restricted page: ${tab.url}`,
            },
          };
        }

        // Support both function form: () => document.title and expression: document.title
        const funcRegex = /^\s*(async\s+)?(function\b|\([^)]*\)\s*=>|[A-Za-z_$][\w$]*\s*=>)/;
        let expression = script.trim();
        if (funcRegex.test(expression)) {
          expression = `(${expression})()`;
        }

        const hasDebugger = await ensureDebuggerAttached(tabId);
        if (hasDebugger) {
          try {
            const evalRes = await chrome.debugger.sendCommand({ tabId }, 'Runtime.evaluate', {
              expression,
              awaitPromise: true,
              returnByValue: true,
            });

            if (evalRes.exceptionDetails) {
              const errMsg =
                evalRes.exceptionDetails.exception?.description ||
                evalRes.exceptionDetails.text ||
                'script_error';
              return {
                type: 'res',
                id,
                error: { code: 'script_error', message: errMsg },
              };
            }

            return {
              type: 'res',
              id,
              result: {
                value: evalRes.result ? evalRes.result.value : null,
              },
            };
          } catch (e) {
            console.warn('Debugger Runtime.evaluate failed, trying fallback:', e);
          }
        }

        // Fallback via scripting in MAIN world
        try {
          const results = await chrome.scripting.executeScript({
            target: { tabId },
            world: 'MAIN',
            func: (codeStr) => {
              const fnRegex = /^\s*(async\s+)?(function\b|\([^)]*\)\s*=>|[A-Za-z_$][\w$]*\s*=>)/;
              if (fnRegex.test(codeStr)) {
                return (0, eval)(`(${codeStr})()`);
              }
              return (0, eval)(codeStr);
            },
            args: [script],
          });

          return {
            type: 'res',
            id,
            result: {
              value: results && results[0] ? results[0].result : null,
            },
          };
        } catch (e) {
          return {
            type: 'res',
            id,
            error: { code: 'script_error', message: e.message || String(e) },
          };
        }
      }

      default:
        return {
          type: 'res',
          id,
          error: {
            code: 'internal_error',
            message: `Method '${method}' not implemented yet`,
          },
        };
    }
  } catch (err) {
    return {
      type: 'res',
      id,
      error: {
        code: 'internal_error',
        message: err.message || String(err),
      },
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
    // Send hello handshake
    const hello = {
      type: 'hello',
      label: config.label,
      profile: config.label,
      instanceId: config.instanceId,
      token: config.token,
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
      setBadge('ON', '#2ecc71');
      reconnectDelay = 1000;
      startPing();
      return;
    }

    if (msg.type === 'hello_error') {
      setBadge('!', '#e74c3c');
      stopPing();
      const code = msg.code || msg.error;
      if (code === 'label_in_use') {
        if (chrome.runtime.openOptionsPage) {
          chrome.runtime.openOptionsPage();
        }
      }
      // Fatal handshake errors: don't loop endlessly
      if (
        code === 'bad_token' ||
        code === 'bad_label' ||
        code === 'label_in_use' ||
        code === 'bad_protocol'
      ) {
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
    setBadge('OFF', '#95a5a6');
    scheduleReconnect();
  };

  ws.onerror = (err) => {
    console.warn('WebSocket error:', err);
    try {
      ws.close();
    } catch {
      // ignore
    }
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

// Keep-alive using alarms (alarms can wake up service worker)
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

// Message listener from options page
chrome.runtime.onMessage.addListener((req, sender, sendResponse) => {
  if (req.type === 'reconnect') {
    if (ws) {
      try {
        ws.close();
      } catch {
        // ignore
      }
    }
    reconnectDelay = 1000;
    connect();
    sendResponse({ status: 'ok' });
  } else if (req.type === 'getStatus') {
    sendResponse({
      connected: ws && ws.readyState === WebSocket.OPEN,
      activeTabIds: Array.from(agentTabIds),
    });
  }
  return true;
});

// Startup
connect();
