import { snapshotFn } from './lib/snapshot.js';

let ws = null;
let pingInterval = null;
let reconnectTimeout = null;
let reconnectDelay = 1000;
const MAX_RECONNECT_DELAY = 30000;

let agentWindowId = null;
const agentTabIds = new Set();

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

// Configuration loader
async function getConfig() {
  const data = await chrome.storage.local.get([
    'label',
    'token',
    'daemonUrl',
    'windowMode',
    'instanceId',
  ]);
  let instanceId = data.instanceId;
  if (!instanceId) {
    instanceId = generateUUID();
    await chrome.storage.local.set({ instanceId });
  }
  return {
    label: (data.label || '').trim(),
    token: (data.token || '').trim(),
    daemonUrl: (data.daemonUrl || 'ws://127.0.0.1:3025/ext').trim(),
    windowMode: data.windowMode || 'dedicated',
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

// Clean up closed tabs from our set
chrome.tabs.onRemoved.addListener((tabId) => {
  agentTabIds.delete(tabId);
});

chrome.windows.onRemoved.addListener((windowId) => {
  if (agentWindowId === windowId) {
    agentWindowId = null;
  }
});

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

  return new Promise((resolve, reject) => {
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
          id,
          result: {
            tabId: updated.id,
            url: updated.url || '',
            title: updated.title || '',
          },
        };
      }

      case 'tabs.list': {
        const tabs = [];
        for (const tabId of Array.from(agentTabIds)) {
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
        return { id, result: {} };
      }

      case 'page.navigate': {
        const tabId = params.tabId;
        await chrome.tabs.update(tabId, { url: params.url });
        await waitForTabLoad(tabId, 30000);
        await sleep(1000); // 1s settling time matching rod backend
        return { id, result: {} };
      }

      case 'page.snapshot': {
        const tabId = params.tabId;
        const tab = await chrome.tabs.get(tabId);
        if (!tab) {
          return {
            id,
            error: { code: 'tab_not_found', message: `Tab ${tabId} not found` },
          };
        }

        // Check for restricted URLs
        const url = tab.url || '';
        if (
          url.startsWith('chrome://') ||
          url.startsWith('chrome-extension://') ||
          url.startsWith('https://chromewebstore.google.com')
        ) {
          return {
            id,
            error: {
              code: 'restricted_page',
              message: `Cannot snapshot restricted page: ${url}`,
            },
          };
        }

        const results = await chrome.scripting.executeScript({
          target: { tabId },
          func: snapshotFn,
        });

        const elements = results && results[0] ? results[0].result : '';
        return {
          id,
          result: {
            url: tab.url || '',
            title: tab.title || '',
            elements: elements || '',
          },
        };
      }

      default:
        return {
          id,
          error: {
            code: 'internal_error',
            message: `Method '${method}' not implemented yet`,
          },
        };
    }
  } catch (err) {
    return {
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
      // Fatal handshake errors: don't loop endlessly
      if (
        msg.error === 'bad_token' ||
        msg.error === 'bad_label' ||
        msg.error === 'label_in_use'
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
    });
  }
  return true;
});

// Startup
connect();
