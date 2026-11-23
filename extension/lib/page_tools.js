/**
 * page_tools.js - navigate, javascript_tool, resize_window, file_upload and upload_image.
 */
import { ToolError } from './errors.js';
import { frameToViewport, getStoredScreenshot } from './computer.js';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// ---------------------------------------------------------------------------
// navigate
// ---------------------------------------------------------------------------
// tabId -> { accept, outcome }   (set while a navigation is in flight)
const beforeunloadPolicy = new Map();

// Called from the debugger event listener. Returns true when the dialog was a beforeunload one.
export async function handleBeforeunloadDialog(tabId, params, sendCommand) {
  if (params.type !== 'beforeunload') return false;
  const policy = beforeunloadPolicy.get(tabId) || { accept: false };
  policy.outcome = { action: policy.accept ? 'accepted' : 'dismissed', url: params.url };
  beforeunloadPolicy.set(tabId, policy);
  try {
    await sendCommand(tabId, 'Page.handleJavaScriptDialog', { accept: policy.accept });
  } catch {}
  return true;
}

const NAV_TIMEOUT_MS = 25000; // stay below the daemon's 30s RPC timeout
const sameHost = (a, b) => {
  const host = (u) => new URL(u).host.replace(/^www\./, '');
  try {
    return host(a) === host(b);
  } catch {
    return true;
  }
};

async function runNavigation(tabId, force, action, targetUrl) {
  const policy = { accept: force, outcome: null };
  beforeunloadPolicy.set(tabId, policy);
  // status changes are caught through events: the "loading" phase of a fast page is shorter than
  // any sensible polling interval
  let sawLoading = false;
  let failed = false;
  const onUpdated = (id, change) => {
    if (id === tabId && change.status === 'loading') sawLoading = true;
  };
  const onError = (details) => {
    if (details.tabId === tabId && details.frameId === 0 && details.error !== 'net::ERR_ABORTED') failed = true;
  };
  chrome.tabs.onUpdated?.addListener(onUpdated);
  chrome.webNavigation?.onErrorOccurred?.addListener(onError);
  try {
    const before = await chrome.tabs.get(tabId);
    await action();
    // give a "Leave site?" dialog a moment to appear and be answered
    for (let waited = 0; waited < 300 && !policy.outcome; waited += 50) await sleep(50);
    if (policy.outcome && policy.outcome.action === 'dismissed') {
      return {
        kind: 'blocked',
        error: `Navigation was blocked by a "Leave site?" dialog — the page at ${policy.outcome.url || before.url} has unsaved changes. The page is still open and unchanged. Either address the unsaved state first, or retry with force: true to discard it and navigate anyway.`,
      };
    }
    let arrival = 'pending';
    let landedAt;
    const started = Date.now();
    while (Date.now() - started < NAV_TIMEOUT_MS) {
      const tab = await chrome.tabs.get(tabId);
      const url = tab.pendingUrl || tab.url || '';
      if (tab.status === 'loading') sawLoading = true;
      if (tab.status === 'complete' && (sawLoading || url !== before.url || targetUrl === null)) {
        arrival = failed || url.startsWith('chrome-error://') ? 'failed' : 'committed';
        landedAt = tab.url;
        break;
      }
      await sleep(50);
    }
    const accepted = policy.outcome && policy.outcome.action === 'accepted';
    return {
      kind: accepted ? 'accepted' : 'none',
      arrival,
      landedAt,
      suffix: ' (discarded a "Leave site?" dialog — the page had unsaved changes that are now lost)',
    };
  } finally {
    chrome.tabs.onUpdated?.removeListener(onUpdated);
    chrome.webNavigation?.onErrorOccurred?.removeListener(onError);
    beforeunloadPolicy.delete(tabId);
  }
}

function navigationMessage(infix, target, res) {
  if (res.arrival === 'committed') return `Navigated${infix}${target}${res.kind === 'accepted' ? res.suffix : ''}`;
  const dialog = res.kind === 'accepted' ? ' The page\'s "Leave site?" dialog was accepted, so its unsaved changes may be lost.' : '';
  if (res.arrival === 'failed') {
    return `Navigation${infix}${target} ended without loading a page: it may have started a download, returned nothing or failed.${dialog} Check what the tab shows before acting on it.`;
  }
  return `Navigation${infix}${target} has not finished, and the tab may still show the page it was on.${dialog} Check where the tab is in a few seconds before acting on it: the page may still be loading.`;
}

export async function navigateTab(tabId, params, deps) {
  try {
    return await navigateTabInner(tabId, params, deps);
  } catch (err) {
    if (err instanceof ToolError) throw err;
    throw new ToolError('navigate_exception', `Failed to navigate: ${err instanceof Error ? err.message : 'Unknown error'}`);
  }
}

async function navigateTabInner(tabId, params, deps) {
  const raw = typeof params.url === 'string' ? params.url.trim() : '';
  if (!raw) throw new ToolError('navigate_exception', 'Failed to navigate: URL parameter is required');
  await deps.ensureAttached(tabId).catch(() => {});
  const lower = raw.toLowerCase();
  const force = params.force === true;

  if (lower === 'back' || lower === 'forward') {
    const res = await runNavigation(tabId, force, () => (lower === 'back' ? chrome.tabs.goBack(tabId) : chrome.tabs.goForward(tabId)), null);
    if (res.kind === 'blocked') throw new ToolError('navigate_beforeunload_blocked', res.error);
    const tab = await chrome.tabs.get(tabId);
    const to = res.arrival === 'committed' && tab.url ? ` to ${tab.url}` : '';
    return { output: navigationMessage(` ${lower}`, to, res) };
  }

  let target = raw;
  if (!/^https?:\/\//i.test(target)) target = `https://${target}`;
  try {
    new URL(target);
  } catch {
    throw new ToolError('navigate_exception', `Failed to navigate: Invalid URL: ${raw}`);
  }
  const res = await runNavigation(tabId, force, () => chrome.tabs.update(tabId, { url: target }), target);
  if (res.kind === 'blocked') throw new ToolError('navigate_beforeunload_blocked', res.error);
  let output = navigationMessage('', ` to ${target}`, res);
  if (res.arrival === 'committed' && res.landedAt && /^https?:/i.test(res.landedAt) && !sameHost(res.landedAt, target)) {
    output += `. The tab was redirected to a page on "${new URL(res.landedAt).origin}", and that page may move it again.`;
  }
  return { output };
}

// ---------------------------------------------------------------------------
// javascript_tool
// ---------------------------------------------------------------------------
const JS_TIMEOUT_MS = 30000;
const JS_MAX_OUTPUT = 51200;

function describeResult(result) {
  if (!result) return 'undefined';
  if (result.type === 'undefined') return 'undefined';
  if (result.type === 'object' && result.subtype === 'null') return 'null';
  if (result.type === 'function') return result.description || '[Function]';
  if (result.type === 'object') {
    if (result.subtype === 'node') return result.description || '[DOM Node]';
    if (result.value !== undefined) return JSON.stringify(result.value, null, 2);
    if (result.subtype === 'array') return result.description || '[Array]';
    return result.description || '{}';
  }
  if (result.value !== undefined) return typeof result.value === 'string' ? result.value : JSON.stringify(result.value, null, 2);
  return result.description || String(result.value);
}

export async function runJavascript(tabId, params, deps) {
  if (params.action !== 'javascript_exec') throw new ToolError('invalid_params', "'javascript_exec' is the only supported action");
  const code = params.text;
  if (!code) throw new ToolError('invalid_params', 'Code parameter is required');
  const tab = await chrome.tabs.get(tabId);
  if (!tab.url) throw new ToolError('js_exception', 'Failed to execute JavaScript: No URL available for active tab');
  const scheme = /^([a-z-]+:)/i.exec(tab.url);
  if (scheme && !/^https?:$/i.test(scheme[1]) && /^(chrome|chrome-extension|chrome-error|devtools|about|edge):$/i.test(scheme[1])) {
    throw new ToolError(
      'chrome_protected_page',
      `JavaScript execution is not allowed on ${scheme[1]}// pages. Navigate to a regular web page (http:// or https://) first, then retry.`
    );
  }

  const evaluate = (expression, replMode) =>
    deps.sendCommand(tabId, 'Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true, replMode, timeout: JS_TIMEOUT_MS });
  let res;
  try {
    res = await evaluate(`{${code}\n}`, true);
    const ex = res.exceptionDetails && res.exceptionDetails.exception;
    if (ex && ex.className === 'SyntaxError' && /Illegal return statement/.test(ex.description || '')) {
      res = await evaluate(`(async()=>{\n${code}\n})()`, false);
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : 'Unknown error';
    const hint = /navigated or closed/i.test(message)
      ? ' The tab navigated or was closed mid-evaluation; call tabs_context to refresh tab IDs, then re-run against the current page (avoid long sleeps that span a navigation).'
      : '';
    throw new ToolError('js_exception', `Failed to execute JavaScript: ${message}${hint}`);
  }

  if (res.exceptionDetails) {
    const ex = res.exceptionDetails.exception;
    const timedOut = ex && ex.description && ex.description.includes('execution was terminated');
    const text = timedOut
      ? `Execution timeout: Code exceeded ${JS_TIMEOUT_MS / 1000}-second limit`
      : (ex && (ex.description || ex.value)) || res.exceptionDetails.text || 'Unknown error';
    throw new ToolError(timedOut ? 'js_timeout' : 'js_execution_error', `JavaScript execution error: ${text}`);
  }
  let out = describeResult(res.result);
  if (out.length > JS_MAX_OUTPUT) out = out.substring(0, JS_MAX_OUTPUT) + '\n[OUTPUT TRUNCATED: Exceeded 50KB limit]';
  return { output: out };
}

// ---------------------------------------------------------------------------
// resize_window
// ---------------------------------------------------------------------------
export async function resizeWindow(tabId, params) {
  const { width, height } = params;
  if (!width || !height) throw new ToolError('invalid_params', 'Both width and height parameters are required');
  if (typeof width !== 'number' || typeof height !== 'number') throw new ToolError('invalid_params', 'Width and height must be numbers');
  if (width <= 0 || height <= 0) throw new ToolError('invalid_params', 'Width and height must be positive numbers');
  if (width > 7680 || height > 4320) {
    throw new ToolError('invalid_params', 'Dimensions exceed 8K resolution limit. Maximum dimensions are 7680x4320');
  }
  const tab = await chrome.tabs.get(tabId);
  if (!tab.windowId) throw new Error('Tab does not have an associated window');
  await chrome.windows.update(tab.windowId, { width: Math.floor(width), height: Math.floor(height) });
  return { output: `Successfully resized window containing tab ${tabId} to ${Math.floor(width)}x${Math.floor(height)} pixels` };
}

// ---------------------------------------------------------------------------
// file_upload / upload_image (injected into the page)
// ---------------------------------------------------------------------------
export function uploadFilesInPage(ref, files) {
  try {
    const map = window.__klikRefMap;
    if (!map || !map[ref]) return { error: `Element ref not found: "${ref}". The element may have been removed from the page.`, errorCode: 'ref_stale' };
    const el = map[ref].deref();
    if (!el) {
      delete map[ref];
      return { error: `Element has been garbage collected: "${ref}"`, errorCode: 'ref_stale' };
    }
    if (!document.contains(el)) {
      delete map[ref];
      return { error: `Element is no longer in the document: "${ref}"`, errorCode: 'ref_stale' };
    }
    const tag = el.localName;
    const type = el.type;
    if (tag !== 'input' || type !== 'file') {
      const shown = /^[a-z][a-z0-9]*$/.test(tag) ? tag : 'custom element';
      const typeAttr = (tag === 'input' || tag === 'button') && type ? ` type="${type}"` : '';
      return {
        error: `Element is not a file input. Found: <${shown}${typeAttr}>. To find one, call read_page with filter "all" and look for type="file".`,
        errorCode: 'element_unsupported',
      };
    }
    const dt = new DataTransfer();
    let bytes = 0;
    for (const f of files) {
      const bin = atob(f.data);
      const arr = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
      bytes += arr.length;
      dt.items.add(new File([arr], f.name, { type: f.mimeType || 'application/octet-stream', lastModified: Date.now() }));
    }
    el.files = dt.files;
    el.focus();
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
    return { output: `Uploaded ${files.length} file(s) to file input: ${files.map((f) => f.name).join(', ')} (${Math.round(bytes / 1024)} KB total)` };
  } catch (e) {
    return { error: `Error uploading file(s): ${e instanceof Error ? e.message : String(e)}`, errorCode: 'injected_script_failed' };
  }
}

const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;

export async function fileUpload(tabId, params) {
  const files = params.files;
  if (!Array.isArray(files) || files.length === 0) {
    throw new ToolError('invalid_params', 'files parameter is required and must be a non-empty array');
  }
  let total = 0;
  const clean = [];
  for (const f of files) {
    if (!f || typeof f.data !== 'string' || typeof f.name !== 'string' || !f.name) {
      throw new ToolError('invalid_params', 'each file must have `data` and `name`');
    }
    if (f.name.length > 255 || /[\\/]/.test(f.name) || f.name === '.' || f.name === '..') {
      throw new ToolError('invalid_params', 'each file `name` must be a plain file name (no directories, at most 255 characters)');
    }
    total += Math.floor((f.data.length * 3) / 4);
    if (total > MAX_UPLOAD_BYTES) {
      throw new ToolError('input_invalid', 'Total upload size exceeds 10 MB; use smaller files or split across multiple file_upload calls.');
    }
    clean.push({ data: f.data, name: f.name, ...(typeof f.mimeType === 'string' && { mimeType: f.mimeType }) });
  }
  if (!params.ref) throw new ToolError('invalid_params', 'ref parameter is required');
  const results = await chrome.scripting.executeScript({ target: { tabId }, func: uploadFilesInPage, args: [params.ref, clean] });
  const res = results && results[0] ? results[0].result : null;
  if (!res) throw new ToolError('injected_script_failed', 'Failed to execute script to upload file(s)');
  if (res.error) throw new ToolError(res.errorCode || 'injected_script_failed', res.error);
  return { output: res.output };
}

export function uploadImageInPage(ref, viewportPoint, framePoint, base64, filename, mime) {
  try {
    let el = null;
    if (viewportPoint) {
      const shown = framePoint || viewportPoint;
      el = document.elementFromPoint(viewportPoint[0], viewportPoint[1]);
      if (!el) return { error: `No element found at coordinates (${shown[0]}, ${shown[1]})` };
      if (el.tagName === 'IFRAME') {
        try {
          const doc = el.contentDocument || (el.contentWindow ? el.contentWindow.document : null);
          if (doc) {
            const box = el.getBoundingClientRect();
            const inner = doc.elementFromPoint(viewportPoint[0] - box.left, viewportPoint[1] - box.top);
            if (inner) el = inner;
          }
        } catch {}
      }
    } else {
      if (!ref) return { error: 'Neither coordinate nor elementRef provided' };
      const weak = window.__klikRefMap && window.__klikRefMap[ref];
      el = weak ? weak.deref() || null : null;
      if (!el || !document.contains(el)) {
        if (window.__klikRefMap) delete window.__klikRefMap[ref];
        return { error: `No element found with reference: "${ref}". The element may have been removed from the page.` };
      }
      const r = el.getBoundingClientRect();
      el.scrollIntoView({
        behavior: 'instant',
        block: r.bottom <= 0 || r.top >= innerHeight ? 'center' : 'nearest',
        inline: r.right <= 0 || r.left >= innerWidth ? 'center' : 'nearest',
      });
    }
    const bin = atob(base64);
    const arr = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
    const blob = new Blob([arr], { type: mime });
    const file = new File([blob], filename, { type: mime, lastModified: Date.now() });
    const dt = new DataTransfer();
    dt.items.add(file);
    const kb = Math.round(blob.size / 1024);
    if (el.tagName === 'INPUT' && el.type === 'file') {
      el.files = dt.files;
      el.focus();
      el.dispatchEvent(new Event('change', { bubbles: true }));
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new CustomEvent('filechange', { bubbles: true, detail: { files: dt.files } }));
      return { output: `Successfully uploaded image "${filename}" (${kb}KB) to file input` };
    }
    el.focus();
    let x;
    let y;
    if (viewportPoint) {
      x = viewportPoint[0];
      y = viewportPoint[1];
    } else {
      const r = el.getBoundingClientRect();
      x = r.left + r.width / 2;
      y = r.top + r.height / 2;
    }
    for (const type of ['dragenter', 'dragover', 'drop']) {
      el.dispatchEvent(
        new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt, clientX: x, clientY: y, screenX: x + window.screenX, screenY: y + window.screenY })
      );
    }
    const [shownX, shownY] = framePoint || [x, y];
    return { output: `Successfully dropped image "${filename}" (${kb}KB) onto element at (${Math.round(shownX)}, ${Math.round(shownY)})` };
  } catch (e) {
    return { error: `Error uploading image: ${e instanceof Error ? e.message : 'Unknown error'}` };
  }
}

export async function uploadImage(tabId, params) {
  if (!params.imageId) throw new ToolError('invalid_params', 'imageId parameter is required');
  if (!params.ref && !params.coordinate) {
    throw new ToolError('invalid_params', 'Either ref or coordinate parameter is required. Provide ref for targeting specific elements or coordinate for drag & drop to a location.');
  }
  if (params.ref && params.coordinate) {
    throw new ToolError('invalid_params', 'Provide either ref or coordinate, not both. Use ref for specific elements or coordinate for drag & drop.');
  }
  const stored = getStoredScreenshot(params.imageId);
  if (!stored) {
    throw new ToolError(
      'screenshot_bytes_unavailable',
      'The requested screenshot is no longer available (it may have expired, or was not taken in this session). Take a new screenshot with the computer tool and retry the upload with its new ID.'
    );
  }
  let viewportPoint = null;
  if (params.coordinate) viewportPoint = frameToViewport(tabId, params.coordinate, 'upload_image');
  const mime = `image/${stored.format === 'jpeg' ? 'jpeg' : stored.format}`;
  const results = await chrome.scripting.executeScript({
    target: { tabId },
    func: uploadImageInPage,
    args: [params.ref || null, viewportPoint, params.coordinate || null, stored.base64, params.filename || 'image.png', mime],
  });
  const res = results && results[0] ? results[0].result : null;
  if (!res) throw new ToolError('upload_failed', 'Failed to upload image: Failed to execute upload image');
  if (res.error) throw new ToolError('upload_failed', res.error);
  return { output: res.output };
}
