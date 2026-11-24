/**
 * computer.js - the `computer` tool: mouse, keyboard, scrolling and screenshots.
 *
 * Mirrors the behaviour of Claude in Chrome:
 *  - coordinates are pixels of the most recent screenshot ("coordinate frame") and are
 *    mapped back to viewport CSS pixels; `ref` ids from read_page/find can be used instead
 *  - screenshots are JPEG, resized to <= 1568px per side and <= 1568 tokens (28px/token)
 *  - every screenshot gets an id (ss_xxxxxxxx) that upload_image can reuse
 */
import { ToolError } from './errors.js';
import { isPageZoomShortcut, parseKeyToken, parseModifierList } from './keys.js';

export const COMPUTER_ACTIONS = [
  'left_click', 'right_click', 'type', 'screenshot', 'wait', 'scroll', 'key',
  'left_click_drag', 'double_click', 'triple_click', 'zoom', 'scroll_to', 'hover',
];

const SCREENSHOT_LIMITS = { pxPerToken: 28, maxPx: 1568, maxTokens: 1568 };
const JPEG_QUALITY = 75;
const SCROLL_TICK_PX = 100;
const MAX_STORED_SCREENSHOTS = 20;

// tabId -> { screenshotWidth, screenshotHeight, viewportWidth, viewportHeight }
const coordContext = new Map();
// imageId -> { base64, format }
const screenshotStore = new Map();

export function getStoredScreenshot(imageId) {
  return screenshotStore.get(imageId) || null;
}

// Maps a point in the latest screenshot's coordinate frame to viewport CSS pixels.
export function frameToViewport(tabId, point, action) {
  return toViewport(tabId, point, action);
}

export function forgetTabContext(tabId) {
  coordContext.delete(tabId);
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// ---------------------------------------------------------------------------
// Page helpers
// ---------------------------------------------------------------------------
async function injectTree(tabId) {
  await chrome.scripting.executeScript({ target: { tabId }, files: ['lib/page_tree.js'], injectImmediately: true });
}

async function runInPage(tabId, func, args = []) {
  const results = await chrome.scripting.executeScript({ target: { tabId }, func, args, injectImmediately: true });
  return results && results[0] ? results[0].result : undefined;
}

async function viewportInfo(tabId) {
  const info = await runInPage(tabId, () => ({
    width: window.innerWidth,
    height: window.innerHeight,
    dpr: window.devicePixelRatio || 1,
    scrollX: window.scrollX,
    scrollY: window.scrollY,
  }));
  if (!info) throw new ToolError('screenshot_failed', 'Failed to get viewport information');
  return info;
}

// Largest [w, h] with the same aspect ratio that respects the pixel and token budget.
export function fitToBudget(w, h, limits = SCREENSHOT_LIMITS) {
  const tokens = (a, b) => Math.ceil(a / limits.pxPerToken) * Math.ceil(b / limits.pxPerToken);
  if (w <= limits.maxPx && h <= limits.maxPx && tokens(w, h) <= limits.maxTokens) return [w, h];
  if (h > w) {
    const [a, b] = fitToBudget(h, w, limits);
    return [b, a];
  }
  const ratio = w / h;
  let lo = 1;
  let hi = w;
  for (;;) {
    if (lo + 1 >= hi) return [lo, Math.max(Math.round(lo / ratio), 1)];
    const mid = Math.floor((lo + hi) / 2);
    const midH = Math.max(Math.round(mid / ratio), 1);
    if (mid <= limits.maxPx && tokens(mid, midH) <= limits.maxTokens) lo = mid;
    else hi = mid;
  }
}

async function imageSize(base64, mime) {
  try {
    const blob = await (await fetch(`data:${mime};base64,${base64}`)).blob();
    const bmp = await createImageBitmap(blob);
    const size = { width: bmp.width, height: bmp.height };
    bmp.close();
    return size;
  } catch {
    return null;
  }
}

function newImageId() {
  return 'ss_' + Date.now().toString().slice(-4) + Math.random().toString(36).substring(2, 7);
}

function normalizeScale(scale) {
  return typeof scale === 'number' && scale >= 0.1 && scale <= 1 ? scale : 1;
}

// ---------------------------------------------------------------------------
// Coordinates
// ---------------------------------------------------------------------------
function toViewport(tabId, point, action) {
  const ctx = coordContext.get(tabId);
  const [x, y] = point;
  if (!ctx) return [x, y];
  if (!Number.isFinite(x) || !Number.isFinite(y) || x < 0 || y < 0 || x >= ctx.screenshotWidth + 2 || y >= ctx.screenshotHeight + 2) {
    throw new ToolError(
      'coordinate_out_of_bounds',
      `${action}: Coordinate (${Math.round(x)}, ${Math.round(y)}) is outside the coordinate frame (${ctx.screenshotWidth}x${ctx.screenshotHeight}). Coordinates are pixels in the full-resolution frame, never in a scaled image's own pixels — if the page or window changed, take a new screenshot first.`
    );
  }
  return [
    Math.round((x * ctx.viewportWidth) / ctx.screenshotWidth),
    Math.round((y * ctx.viewportHeight) / ctx.screenshotHeight),
  ];
}

function requirePoint(value, name, action) {
  if (!Array.isArray(value) || value.length !== 2 || !value.every((n) => typeof n === 'number')) {
    throw new ToolError('invalid_params', `${name} parameter is required for ${action} action and must be [x, y]`);
  }
  return value;
}

async function resolveRef(tabId, ref) {
  await injectTree(tabId);
  const res = await runInPage(tabId, (r) => window.__klikResolveRef(r), [ref]);
  if (!res || !res.success) {
    throw new ToolError('computer_element_not_found', (res && res.error) || 'Failed to resolve element reference');
  }
  if (!res.hasBox) {
    throw new ToolError(
      'computer_element_not_found',
      `The element with reference "${ref}" is not shown on the page right now. No mouse event was sent. Read the page again before you retry.`
    );
  }
  return res.coordinates;
}

// Target of a click/hover: `ref` wins, otherwise `coordinate`.
async function resolveTarget(tabId, params, action) {
  if (params.ref) {
    const point = await resolveRef(tabId, params.ref);
    return { point, label: `element ${params.ref}` };
  }
  if (!params.coordinate) {
    throw new ToolError('invalid_params', `Either ref or coordinate parameter is required for ${action} action`);
  }
  const coordinate = requirePoint(params.coordinate, 'coordinate', action);
  return {
    point: toViewport(tabId, coordinate, action),
    label: `(${Math.round(coordinate[0])}, ${Math.round(coordinate[1])})`,
    atCoordinate: true,
  };
}

// ---------------------------------------------------------------------------
// Screenshots
// ---------------------------------------------------------------------------
// In-tab UI (glow, tag, cursor) lives in lib/indicator_page.js; failures never affect an action.
export async function ui(tabId, method, args = []) {
  try {
    await chrome.scripting.executeScript({ target: { tabId }, files: ['lib/indicator_page.js'], injectImmediately: true });
    return await runInPage(tabId, (m, a) => (window.__klikUi && window.__klikUi[m] ? window.__klikUi[m](...a) : undefined), [method, args]);
  } catch {
    return undefined;
  }
}

async function withHiddenIndicators(tabId, fn) {
  await ui(tabId, 'capture', [true]);
  await sleep(50);
  try {
    return await fn();
  } finally {
    await ui(tabId, 'capture', [false]);
  }
}

async function captureScreenshot(tabId, deps, scale, { updateContext = true, store = true } = {}) {
  const l = normalizeScale(scale);
  const vp = await viewportInfo(tabId);
  const S = Math.round(vp.width * vp.dpr);
  const A = Math.round(vp.height * vp.dpr);
  const [E] = fitToBudget(S, A);
  const j = Math.max(1, Math.round(E * l));
  const C = Math.min(1, j / S);
  const frame =
    l < 1 ? { width: E, height: Math.max(1, Math.round(A * Math.min(1, E / S))) } : null;

  const shot = await withHiddenIndicators(tabId, () =>
    deps.sendCommand(tabId, 'Page.captureScreenshot', {
      format: 'jpeg',
      quality: JPEG_QUALITY,
      captureBeyondViewport: false,
      fromSurface: true,
      clip: { x: vp.scrollX, y: vp.scrollY, width: vp.width, height: vp.height, scale: C },
    })
  );
  if (!shot || !shot.data) throw new ToolError('screenshot_failed', 'Failed to capture screenshot via CDP');

  const size = (await imageSize(shot.data, 'image/jpeg')) || { width: j, height: Math.max(1, Math.round(A * C)) };
  const frameW = frame ? frame.width : size.width;
  const frameH = frame ? frame.height : size.height;
  if (updateContext) {
    coordContext.set(tabId, {
      screenshotWidth: frameW,
      screenshotHeight: frameH,
      viewportWidth: vp.width,
      viewportHeight: vp.height,
    });
  }

  const imageId = newImageId();
  if (store) {
    screenshotStore.set(imageId, { base64: shot.data, format: 'jpeg' });
    while (screenshotStore.size > MAX_STORED_SCREENSHOTS) {
      screenshotStore.delete(screenshotStore.keys().next().value);
    }
  }

  const note = frame ? ` — ${l}-scale view; coordinate frame: ${frameW}x${frameH}.` : '';
  return {
    output: `Successfully captured screenshot (${size.width}x${size.height}, jpeg) - ID: ${imageId}${note}`,
    base64Image: shot.data,
    imageFormat: 'jpeg',
    imageId,
    frameWidth: frameW,
    frameHeight: frameH,
    imageWidth: size.width,
    imageHeight: size.height,
  };
}

// A reduced-size screenshot used as one GIF frame; does not touch the coordinate context.
export async function captureFrame(tabId, deps) {
  const shot = await captureScreenshot(tabId, deps, 0.6, { updateContext: false, store: false });
  const ctx = coordContext.get(tabId);
  return {
    base64: shot.base64Image,
    imageWidth: shot.imageWidth,
    imageHeight: shot.imageHeight,
    // action coordinates are expressed in the latest screenshot's frame
    frameWidth: ctx ? ctx.screenshotWidth : shot.frameWidth,
    frameHeight: ctx ? ctx.screenshotHeight : shot.frameHeight,
  };
}

async function doZoom(tabId, params, deps) {
  const region = params.region;
  if (!Array.isArray(region) || region.length !== 4 || !region.every((n) => typeof n === 'number')) {
    throw new ToolError('invalid_params', 'Region parameter is required for zoom action and must be [x0, y0, x1, y1]');
  }
  const [rx0, ry0, rx1, ry1] = region;
  if (rx0 < 0 || ry0 < 0 || rx1 <= rx0 || ry1 <= ry0) {
    throw new ToolError('invalid_params', 'Invalid region coordinates: x0 and y0 must be non-negative, and x1 > x0, y1 > y0');
  }
  const [x0, y0] = toViewport(tabId, [rx0, ry0], 'zoom');
  const [x1, y1] = toViewport(tabId, [rx1, ry1], 'zoom');
  const vp = await viewportInfo(tabId);
  if (x1 > vp.width || y1 > vp.height) {
    throw new ToolError(
      'coordinate_out_of_bounds',
      `Region exceeds viewport boundaries (${vp.width}x${vp.height}). Please choose a region within the visible viewport.`
    );
  }
  const w = x1 - x0;
  const h = y1 - y0;
  const px = Math.round(w * vp.dpr);
  const py = Math.round(h * vp.dpr);
  const l = normalizeScale(params.scale);
  const [fw, fh] = fitToBudget(px, py);
  const targetW = Math.max(1, Math.round(fw * l));
  const targetH = Math.max(1, Math.round(fh * l));
  const clipScale = Math.min(1, targetW / px, targetH / py);

  const shot = await withHiddenIndicators(tabId, () =>
    deps.sendCommand(tabId, 'Page.captureScreenshot', {
      format: 'png',
      captureBeyondViewport: false,
      fromSurface: true,
      clip: { x: vp.scrollX + x0, y: vp.scrollY + y0, width: w, height: h, scale: clipScale },
    })
  );
  if (!shot || !shot.data) throw new ToolError('screenshot_failed', 'Failed to capture zoomed screenshot via CDP');
  const size = (await imageSize(shot.data, 'image/png')) || { width: Math.round(px * clipScale), height: Math.round(py * clipScale) };
  return {
    output: `Successfully captured zoomed screenshot of region (${Math.round(rx0)},${Math.round(ry0)}) to (${Math.round(rx1)},${Math.round(ry1)}) - ${size.width}x${size.height} pixels`,
    base64Image: shot.data,
    imageFormat: 'png',
  };
}

// ---------------------------------------------------------------------------
// Input
// ---------------------------------------------------------------------------
function mouse(deps, tabId, params) {
  return deps.sendCommand(tabId, 'Input.dispatchMouseEvent', params);
}

// What the agent says it is doing (action_summary), trimmed for the tag; falls back to a generic label.
export function summaryLabel(params, fallback) {
  const t = params && typeof params.action_summary === 'string' ? params.action_summary.trim() : '';
  return t ? t.slice(0, 120) : fallback;
}

// Purely cosmetic: never let it block the action (hidden tabs do not run animation frames).
async function showCursor(tabId, point, isClick, label) {
  await Promise.race([ui(tabId, 'cursor', [point[0], point[1], isClick, label]), sleep(400)]);
}

async function doClick(tabId, params, clickCount, deps) {
  const target = await resolveTarget(tabId, params, params.action);
  const [x, y] = target.point;
  const button = params.action === 'right_click' ? 'right' : 'left';
  const buttons = button === 'right' ? 2 : 1;
  const modifiers = parseModifierList(params.modifiers);

  // Links that open a new tab: Chrome puts it in the opener's group, so compare the group's
  // tabs before and after the click and tell the agent about any new one.
  const before = new Set(await deps.groupTabIds());
  await showCursor(tabId, [x, y], true, summaryLabel(params, `Clicking ${target.label}`));
  await mouse(deps, tabId, { type: 'mouseMoved', x, y, button: 'none', buttons: 0, modifiers });
  for (let i = 1; i <= clickCount; i++) {
    await mouse(deps, tabId, { type: 'mousePressed', x, y, button, buttons, clickCount: i, modifiers });
    await sleep(25);
    await mouse(deps, tabId, { type: 'mouseReleased', x, y, button, buttons: 0, clickCount: i, modifiers });
    if (i < clickCount) await sleep(60);
  }
  await sleep(250);
  const opened = (await deps.groupTabIds()).filter((id) => !before.has(id));

  const verb = clickCount === 1 ? 'Clicked' : clickCount === 2 ? 'Double-clicked' : 'Triple-clicked';
  const where = target.atCoordinate ? `at ${target.label}` : `on ${target.label}`;
  const note = opened.length
    ? ` [note: the link opened in a new tab (tab ID ${opened.join(', ')}); pass that tab ID to interact with it]`
    : '';
  return { output: `${verb} ${where}${note}` };
}

async function doHover(tabId, params, deps) {
  const target = await resolveTarget(tabId, params, 'hover');
  const [x, y] = target.point;
  await showCursor(tabId, [x, y], false, summaryLabel(params, `Hovering ${target.label}`));
  await mouse(deps, tabId, { type: 'mouseMoved', x, y, button: 'none', buttons: 0, modifiers: 0 });
  return { output: target.atCoordinate ? `Hovered at ${target.label}` : `Hovered over ${target.label}` };
}

async function pressKey(deps, tabId, parsed) {
  const base = {
    key: parsed.key,
    code: parsed.code,
    windowsVirtualKeyCode: parsed.vk,
    nativeVirtualKeyCode: parsed.vk,
    modifiers: parsed.modifiers,
  };
  await deps.sendCommand(tabId, 'Input.dispatchKeyEvent', {
    ...base,
    type: parsed.text ? 'keyDown' : 'rawKeyDown',
    text: parsed.text || '',
    unmodifiedText: parsed.text || '',
    commands: parsed.commands,
  });
  await deps.sendCommand(tabId, 'Input.dispatchKeyEvent', { ...base, type: 'keyUp' });
}

async function doKey(tabId, params, deps) {
  if (typeof params.text !== 'string' || !params.text.trim()) {
    throw new ToolError('invalid_params', 'Text parameter must be a non-empty string for key action');
  }
  const repeat = params.repeat === undefined ? 1 : params.repeat;
  if (!Number.isInteger(repeat) || repeat < 1) throw new ToolError('invalid_params', 'Repeat parameter must be a positive integer');
  if (repeat > 100) throw new ToolError('invalid_params', 'Repeat parameter cannot exceed 100');

  const tokens = params.text.trim().split(/\s+/);
  for (const token of tokens) {
    if (isPageZoomShortcut(token)) {
      throw new ToolError(
        'page_zoom_shortcut_unsupported',
        `"${token}" was not pressed: page zoom keyboard shortcuts are not supported. To magnify part of the page for closer inspection, use the zoom action with a region instead.`
      );
    }
  }
  const parsed = tokens.map(parseKeyToken);
  for (let i = 0; i < repeat; i++) {
    for (const key of parsed) await pressKey(deps, tabId, key);
  }
  const suffix = repeat > 1 ? ` (repeated ${repeat} times)` : '';
  return { output: `Pressed ${tokens.length} key${tokens.length === 1 ? '' : 's'}: ${tokens.join(' ')}${suffix}` };
}

async function doType(tabId, params, deps) {
  if (typeof params.text !== 'string' || !params.text) {
    throw new ToolError('invalid_params', 'Text parameter must be a non-empty string for type action');
  }
  const parts = params.text.replace(/\r\n?/g, '\n').split('\n');
  for (let i = 0; i < parts.length; i++) {
    if (parts[i]) await deps.sendCommand(tabId, 'Input.insertText', { text: parts[i] });
    if (i < parts.length - 1) await pressKey(deps, tabId, parseKeyToken('Enter'));
  }
  return { output: `Typed "${params.text}"` };
}

async function doScroll(tabId, params, deps) {
  const coordinate = requirePoint(params.coordinate, 'coordinate', 'scroll');
  const [x, y] = toViewport(tabId, coordinate, 'scroll');
  const direction = params.scroll_direction || 'down';
  const ticks = params.scroll_amount === undefined ? 3 : params.scroll_amount;
  if (typeof ticks !== 'number' || ticks < 1 || ticks > 10) {
    throw new ToolError('invalid_params', 'scroll_amount must be a number between 1 and 10');
  }
  let dx = 0;
  let dy = 0;
  if (direction === 'up') dy = -ticks * SCROLL_TICK_PX;
  else if (direction === 'down') dy = ticks * SCROLL_TICK_PX;
  else if (direction === 'left') dx = -ticks * SCROLL_TICK_PX;
  else if (direction === 'right') dx = ticks * SCROLL_TICK_PX;
  else throw new ToolError('invalid_params', `Invalid scroll direction: ${direction}`);

  await mouse(deps, tabId, { type: 'mouseWheel', x, y, deltaX: dx, deltaY: dy, modifiers: 0 });
  await sleep(200);

  const result = { output: `Scrolled ${direction} by ${ticks} ticks at (${Math.round(coordinate[0])}, ${Math.round(coordinate[1])})` };
  try {
    const shot = await captureScreenshot(tabId, deps, undefined);
    result.base64Image = shot.base64Image;
    result.imageFormat = shot.imageFormat;
  } catch {
    // the scroll itself succeeded; the follow-up screenshot is best effort
  }
  return result;
}

async function doDrag(tabId, params, deps) {
  const start = requirePoint(params.start_coordinate, 'start_coordinate', 'left_click_drag');
  const end = requirePoint(params.coordinate, 'coordinate (end position)', 'left_click_drag');
  const [sx, sy] = toViewport(tabId, start, 'left_click_drag');
  const [ex, ey] = toViewport(tabId, end, 'left_click_drag');
  await showCursor(tabId, [sx, sy], true, summaryLabel(params, 'Dragging...'));
  await mouse(deps, tabId, { type: 'mouseMoved', x: sx, y: sy, button: 'none', buttons: 0, modifiers: 0 });
  await mouse(deps, tabId, { type: 'mousePressed', x: sx, y: sy, button: 'left', buttons: 1, clickCount: 1, modifiers: 0 });
  try {
    for (let step = 1; step <= 5; step++) {
      await mouse(deps, tabId, {
        type: 'mouseMoved',
        x: sx + ((ex - sx) * step) / 5,
        y: sy + ((ey - sy) * step) / 5,
        button: 'left',
        buttons: 1,
        modifiers: 0,
      });
      await sleep(30);
    }
  } finally {
    await mouse(deps, tabId, { type: 'mouseReleased', x: ex, y: ey, button: 'left', buttons: 0, clickCount: 1, modifiers: 0 });
  }
  await showCursor(tabId, [ex, ey], false, summaryLabel(params, 'Dropped'));
  return {
    output: `Dragged from (${Math.round(start[0])}, ${Math.round(start[1])}) to (${Math.round(end[0])}, ${Math.round(end[1])})`,
  };
}

async function doScrollTo(tabId, params) {
  if (!params.ref) throw new ToolError('invalid_params', 'ref parameter is required for scroll_to action');
  await resolveRef(tabId, params.ref); // scrolls the element into view as a side effect
  return { output: `Scrolled to element with reference: ${params.ref}` };
}

async function doWait(params) {
  const d = params.duration;
  if (typeof d !== 'number' || d <= 0) throw new ToolError('invalid_params', 'Duration parameter is required and must be positive');
  if (d > 10) throw new ToolError('invalid_params', 'Duration cannot exceed 10 seconds');
  await sleep(Math.round(d * 1000));
  return { output: `Waited for ${d} second${d === 1 ? '' : 's'}` };
}

/**
 * Runs one computer action.
 * deps: { sendCommand(tabId, method, params), groupTabIds() }
 */
export async function runComputerAction(tabId, params, deps) {
  const action = params.action;
  if (!COMPUTER_ACTIONS.includes(action)) {
    throw new ToolError('invalid_params', `Unsupported action: ${action}. Supported: ${COMPUTER_ACTIONS.join(', ')}`);
  }
  // Show what is about to happen in the tab's tag (type/key have no cursor to carry a label).
  if (params.action_summary && action !== 'screenshot' && action !== 'zoom') {
    await Promise.race([ui(tabId, 'show', [summaryLabel(params, undefined)]), sleep(400)]);
  }
  switch (action) {
    case 'left_click':
    case 'right_click':
      return doClick(tabId, params, 1, deps);
    case 'double_click':
      return doClick(tabId, params, 2, deps);
    case 'triple_click':
      return doClick(tabId, params, 3, deps);
    case 'hover':
      return doHover(tabId, params, deps);
    case 'type':
      return doType(tabId, params, deps);
    case 'key':
      return doKey(tabId, params, deps);
    case 'scroll':
      return doScroll(tabId, params, deps);
    case 'left_click_drag':
      return doDrag(tabId, params, deps);
    case 'screenshot':
      return captureScreenshot(tabId, deps, params.scale);
    case 'zoom':
      return doZoom(tabId, params, deps);
    case 'scroll_to':
      return doScrollTo(tabId, params);
    case 'wait':
      return doWait(params);
    default:
      throw new ToolError('invalid_params', `Unsupported action: ${action}`);
  }
}
