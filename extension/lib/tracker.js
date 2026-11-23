/**
 * tracker.js - per-tab console and network capture fed by chrome.debugger events, plus the text
 * formatting used by read_console_messages and read_network_requests.
 */
const MAX_CONSOLE = 1000;
const MAX_NETWORK = 1000;

const consoleByTab = new Map(); // tabId -> [{ type, text, timestamp, url?, lineNumber?, columnNumber?, stackTrace? }]
const networkByTab = new Map(); // tabId -> [{ requestId, url, method, status, failure, type, timestamp }]
const hostByTab = new Map(); // tabId -> host of the top frame
const networkTracking = new Set(); // tabs whose network traffic is being recorded

function push(map, tabId, entry, max) {
  if (!map.has(tabId)) map.set(tabId, []);
  const list = map.get(tabId);
  list.push(entry);
  if (list.length > max) list.shift();
}

function remoteObjectText(arg) {
  if (arg === undefined || arg === null) return '';
  if (arg.value !== undefined) return typeof arg.value === 'string' ? arg.value : JSON.stringify(arg.value);
  if (arg.unserializableValue !== undefined) return String(arg.unserializableValue);
  return arg.description || arg.className || arg.type || '';
}

function formatStack(stack) {
  if (!stack || !Array.isArray(stack.callFrames) || stack.callFrames.length === 0) return undefined;
  return stack.callFrames
    .map((f) => `    at ${f.functionName || '<anonymous>'} (${f.url}:${f.lineNumber + 1}:${f.columnNumber + 1})`)
    .join('\n');
}

function hostOf(url) {
  try {
    return new URL(url).host;
  } catch {
    return '';
  }
}

export function onDebuggerEvent(tabId, method, params) {
  if (method === 'Runtime.consoleAPICalled') {
    const frame = params.stackTrace && params.stackTrace.callFrames && params.stackTrace.callFrames[0];
    const entry = {
      type: params.type || 'log',
      text: (params.args || []).map(remoteObjectText).join(' '),
      timestamp: Date.now(),
    };
    if (frame) {
      entry.url = frame.url;
      entry.lineNumber = frame.lineNumber;
      entry.columnNumber = frame.columnNumber;
    }
    push(consoleByTab, tabId, entry, MAX_CONSOLE);
  } else if (method === 'Runtime.exceptionThrown') {
    const d = params.exceptionDetails || {};
    push(
      consoleByTab,
      tabId,
      {
        type: 'exception',
        text: (d.exception && (d.exception.description || d.exception.value)) || d.text || 'Unknown exception',
        timestamp: Date.now(),
        url: d.url,
        lineNumber: d.lineNumber,
        columnNumber: d.columnNumber,
      },
      MAX_CONSOLE
    );
  } else if (method === 'Network.requestWillBeSent') {
    if (!networkTracking.has(tabId)) return;
    push(
      networkByTab,
      tabId,
      {
        requestId: params.requestId,
        url: params.request.url,
        method: params.request.method,
        type: params.type || 'Other',
        status: null,
        failure: null,
        timestamp: Date.now(),
      },
      MAX_NETWORK
    );
  } else if (method === 'Network.responseReceived') {
    const hit = (networkByTab.get(tabId) || []).find((r) => r.requestId === params.requestId);
    if (hit) hit.status = params.response.status;
  } else if (method === 'Network.loadingFailed') {
    const hit = (networkByTab.get(tabId) || []).find((r) => r.requestId === params.requestId);
    if (hit) hit.failure = params.errorText || 'failed';
  } else if (method === 'Page.frameNavigated' && params.frame && !params.frame.parentId) {
    // Data is scoped to the current domain: drop it when the tab moves to another one.
    const host = hostOf(params.frame.url);
    const previous = hostByTab.get(tabId);
    hostByTab.set(tabId, host);
    if (previous !== undefined && previous !== host) {
      consoleByTab.delete(tabId);
      networkByTab.delete(tabId);
    }
  }
}

// Like Claude in Chrome, network capture starts with the first read_network_requests call.
export function enableNetworkTracking(tabId) {
  const already = networkTracking.has(tabId);
  networkTracking.add(tabId);
  return already;
}

export function forgetTab(tabId) {
  networkTracking.delete(tabId);
  consoleByTab.delete(tabId);
  networkByTab.delete(tabId);
  hostByTab.delete(tabId);
}

export function getConsoleMessages(tabId, onlyErrors, pattern) {
  let list = consoleByTab.get(tabId) || [];
  if (onlyErrors) list = list.filter((m) => m.type === 'error' || m.type === 'exception');
  if (pattern) {
    let re;
    try {
      re = new RegExp(pattern);
    } catch {
      throw new Error(`Invalid regex pattern: ${pattern}`);
    }
    list = list.filter((m) => re.test(m.text));
  }
  return list;
}

export function clearConsole(tabId) {
  consoleByTab.set(tabId, []);
}

export function getNetworkRequests(tabId) {
  return networkByTab.get(tabId) || [];
}

export function clearNetwork(tabId) {
  networkByTab.set(tabId, []);
}

export function formatConsole(messages, { onlyErrors = false, limit = 100 } = {}) {
  if (messages.length === 0) {
    return `No console ${onlyErrors ? 'errors or exceptions' : 'messages'} found for this tab.\n\nNote: Console tracking starts when this tool is first called. If the page loaded before calling this tool, you may need to refresh the page to capture console messages from page load.`;
  }
  const shown = messages.slice(0, limit);
  const body = shown
    .map((m, i) => {
      const time = new Date(m.timestamp).toLocaleTimeString();
      const where =
        m.url && m.lineNumber !== undefined ? ` (${m.url}:${m.lineNumber}${m.columnNumber !== undefined ? ':' + m.columnNumber : ''})` : '';
      let line = `[${i + 1}] [${time}] [${m.type.toUpperCase()}]${where}\n${m.text}`;
      if (m.stackTrace) line += `\nStack trace:\n${m.stackTrace}`;
      return line;
    })
    .join('\n\n');
  const kind = onlyErrors ? 'error/exception messages' : 'console messages';
  const partial = messages.length > limit ? ` (showing first ${limit} of ${messages.length})` : '';
  return `Found ${messages.length} ${kind}${partial}:\n\n${body}`;
}

export function formatNetwork(requests, { urlPattern, limit = 100 } = {}) {
  if (requests.length === 0) {
    const what = urlPattern ? `requests matching "${urlPattern}"` : 'network requests';
    return `No ${what} found for this tab.\n\nNote: Network tracking starts when this tool is first called. If the page loaded before calling this tool, you may need to refresh the page or perform actions that trigger network requests.`;
  }
  const shown = requests.slice(0, limit);
  const body = shown
    .map((r, i) => {
      const status = r.status || (r.failure ? 'none' : 'pending');
      const failed = r.failure ? `\n   failed: ${r.failure}` : '';
      return `${i + 1}. url: ${r.url}\n   method: ${r.method}\n   statusCode: ${status}${failed}`;
    })
    .join('\n\n');
  const filtered = urlPattern ? ` (filtered by URL pattern: "${urlPattern}")` : '';
  const partial = requests.length > limit ? ` (showing first ${limit} of ${requests.length})` : '';
  return `Found ${requests.length} network request${requests.length === 1 ? '' : 's'}${filtered}${partial}:\n\n${body}`;
}
