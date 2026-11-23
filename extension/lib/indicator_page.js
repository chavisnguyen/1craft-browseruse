/**
 * indicator_page.js - classic script injected on demand. Draws what the person sees in a tab while
 * an agent works in it: a pulsing glow around the page, a tag at the top with a Stop button, and a
 * labelled cursor with a ring on clicks. Everything lives in a closed shadow root so page CSS and
 * page scripts cannot reach it.
 *
 * window.__klikUi.show(label)          glow + tag (fades out by itself after IDLE_MS)
 * window.__klikUi.cursor(x, y, click, label)
 * window.__klikUi.capture(on)          hide the UI for a screenshot; only a plain arrow stays visible
 * window.__klikUi.hide()
 */
(function () {
  if (window.__klikUi && window.__klikUi.version === 3) return;
  if (window.__klikUi && window.__klikUi.hide) window.__klikUi.hide();

  var IDLE_MS = 6000;
  var RGB = '198, 97, 63'; // glow, cursor, ring and label
  var TAG_RGB = '185, 88, 55'; // the tag
  var NS = 'http://www.w3.org/2000/svg';

  var host = null;
  var root = null;
  var glow = null;
  var layer = null;
  var cursorEl = null;
  var labelEl = null;
  var ringEl = null;
  var plain = null;
  var wordsEl = null;
  var idleTimer = null;
  var spentTimer = null;
  var last = { x: -100, y: -100 };

  var CSS = [
    ':host { all: initial !important; display: contents !important; }',
    '@keyframes pulse { 0%, 100% { opacity: 0.6; } 50% { opacity: 1; } }',
    '@keyframes ring { from { opacity: 0.95; transform: scale(0.22); } to { opacity: 0; transform: scale(1); } }',
    '@keyframes ring-fade { from { opacity: 0.95; } to { opacity: 0; } }',
    '#glow { position: fixed; inset: 0; pointer-events: none; z-index: 2147483646; opacity: 0; transition: opacity 0.3s ease-in-out; }',
    '#glow[data-on] { opacity: 1; }',
    '#glow[data-captured] { opacity: 0 !important; transition: none !important; }',
    '#glow-inner { position: absolute; inset: 0; will-change: opacity; animation: pulse 2s ease-in-out infinite;',
    '  box-shadow: inset 0 0 22px rgba(' + RGB + ', 0.8), inset 0 0 44px rgba(' + RGB + ', 0.5), inset 0 0 66px rgba(' + RGB + ', 0.25); }',
    '#glow::after { content: ""; position: absolute; inset: 0; box-shadow: inset 0 0 0 3px rgb(' + RGB + '); }',
    '#tag { position: absolute; top: 0; left: 0; right: 0; display: flex; align-items: center; gap: 8px; box-sizing: border-box;',
    '  width: fit-content; max-width: calc(100vw - 16px); height: 47px; margin: 0 auto; padding: 3px 8px 0 14px; border-radius: 0 0 14px 14px;',
    '  direction: ltr; user-select: none; pointer-events: auto; white-space: nowrap; color: #fff; background: rgb(' + TAG_RGB + ');',
    '  filter: drop-shadow(0 2px 5px rgba(0, 0, 0, 0.22)); font: 600 14px/20px system-ui, sans-serif; }',
    '#tag svg { flex: none; fill: currentColor; }',
    '#words { min-width: 0; overflow: hidden; text-overflow: ellipsis; }',
    '#stop { position: relative; display: flex; flex: none; align-items: center; justify-content: center; gap: 2px; box-sizing: border-box;',
    '  height: 28px; margin: 0; padding: 0 9px 0 4px; border: 0; border-radius: 8px; color: inherit; background: #191919;',
    '  font: 435 14px/20px system-ui, sans-serif; cursor: pointer; }',
    '#stop:focus-visible { outline: 2px solid #fff; }',
    '@media (hover: hover) { #stop:hover { background: #3c3c3c; } }',
    '@media (pointer: coarse), (hover: none) { #stop { min-height: 44px; } }',
    '#layer { position: fixed; top: 0; left: 0; direction: ltr; user-select: none; pointer-events: none; z-index: 2147483646;',
    '  opacity: 0; transition: opacity 200ms ease-out; }',
    '#layer[data-on] { opacity: 1; transition-duration: 120ms; }',
    '#layer[data-captured] { opacity: 0 !important; transition: none !important; }',
    '#layer div { position: absolute; top: 0; left: 0; box-sizing: border-box; }',
    '#cursor { will-change: transform; transition: transform 260ms cubic-bezier(0.2, 0, 0, 1); }',
    '#arrow { top: -2px; left: -3px; transform-origin: 3px 2px; filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.28)); }',
    '#arrow svg { display: block; transform-origin: 3px 2px; }',
    '#label { height: 22px; max-width: 240px; padding: 3px 8px; overflow: hidden; color: #fff; background: rgb(' + RGB + ');',
    '  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.18); font: 500 12px/16px system-ui, sans-serif; white-space: nowrap;',
    '  text-overflow: ellipsis; transition: opacity 200ms linear; transform: translate(13px, 16px); border-radius: 3px 10px 10px 10px; }',
    '#label[data-spent] { opacity: 0; }',
    '#label:empty { display: none; }',
    '#ring > div { top: -17px; left: -17px; width: 34px; height: 34px; border: 3px solid rgb(' + RGB + '); border-radius: 50%;',
    '  background: rgba(' + RGB + ', 0.3); animation: ring 650ms ease-out forwards; }',
    '#plain { position: fixed; top: 0; left: 0; pointer-events: none; z-index: 2147483646; display: none;',
    '  transition: transform 180ms cubic-bezier(0.2, 0, 0, 1); will-change: transform; }',
    '#plain[data-on] { display: block; }',
    '@media print { #glow, #layer, #plain { display: none !important; } }',
    '@media (forced-colors: active) { #glow::after { border: 3px solid ButtonText; } #tag, #stop { border: 1px solid ButtonText; } }',
    '@media (prefers-reduced-motion: reduce) { #glow-inner { animation: none; } #cursor, #label { transition: none; } #ring > div { animation-name: ring-fade; } }',
  ].join('\n');

  function svg(tag, attrs) {
    var el = document.createElementNS(NS, tag);
    for (var k in attrs) el.setAttribute(k, attrs[k]);
    return el;
  }

  function ensureRoot() {
    if (host && host.isConnected) return root;
    host = document.createElement('div');
    host.setAttribute('aria-hidden', 'true');
    root = host.attachShadow({ mode: 'closed' });
    var style = document.createElement('style');
    style.textContent = CSS;
    root.appendChild(style);
    (document.body || document.documentElement).appendChild(host);
    glow = layer = cursorEl = labelEl = ringEl = plain = wordsEl = null;
    return root;
  }

  function buildGlow(label) {
    glow = document.createElement('div');
    glow.id = 'glow';
    var inner = document.createElement('div');
    inner.id = 'glow-inner';
    glow.appendChild(inner);

    var tag = document.createElement('div');
    tag.id = 'tag';
    tag.setAttribute('role', 'group');
    var mark = svg('svg', { width: '16', height: '16', viewBox: '0 0 16 16' });
    mark.appendChild(svg('circle', { cx: '8', cy: '8', r: '5' }));
    wordsEl = document.createElement('span');
    wordsEl.id = 'words';
    wordsEl.textContent = label;
    var stop = document.createElement('button');
    stop.id = 'stop';
    stop.type = 'button';
    stop.setAttribute('aria-label', 'Stop');
    var icon = svg('svg', { width: '20', height: '20', viewBox: '0 0 20 20' });
    icon.appendChild(svg('rect', { x: '5.5', y: '5.5', width: '9', height: '9', rx: '2' }));
    stop.appendChild(icon);
    stop.appendChild(document.createTextNode('Stop'));
    stop.addEventListener('click', function (e) {
      if (!e.isTrusted) return;
      e.preventDefault();
      e.stopPropagation();
      try {
        chrome.runtime.sendMessage({ type: 'EMERGENCY_STOP' });
      } catch (err) {}
      hide();
    });
    tag.append(mark, wordsEl, stop);
    for (var i = 0, evs = ['pointerdown', 'mousedown', 'touchstart', 'click', 'dblclick', 'auxclick', 'contextmenu']; i < evs.length; i++) {
      tag.addEventListener(evs[i], function (e) {
        e.stopPropagation();
      });
    }
    glow.appendChild(tag);
    root.appendChild(glow);
  }

  function buildCursor() {
    layer = document.createElement('div');
    layer.id = 'layer';
    ringEl = document.createElement('div');
    ringEl.id = 'ring';
    cursorEl = document.createElement('div');
    cursorEl.id = 'cursor';
    var arrow = document.createElement('div');
    arrow.id = 'arrow';
    var a = svg('svg', { width: '22', height: '22', viewBox: '0 0 18 18' });
    a.appendChild(svg('path', { d: 'M2.2 1.6 L15.4 7.9 L9.5 9.8 L7.2 15.8 Z', fill: 'rgb(' + RGB + ')', stroke: '#fff', 'stroke-width': '1.5', 'stroke-linejoin': 'round' }));
    arrow.appendChild(a);
    labelEl = document.createElement('div');
    labelEl.id = 'label';
    cursorEl.append(arrow, labelEl);
    layer.append(ringEl, cursorEl);
    root.appendChild(layer);

    // the arrow that stays visible in screenshots
    plain = document.createElement('div');
    plain.id = 'plain';
    var p = svg('svg', { width: '20', height: '26', viewBox: '0 0 20 26' });
    p.style.cssText = 'position:absolute;top:0;left:0;overflow:visible';
    var d = 'M0 0 L0 18 L4.5 14 L7.5 21.5 L11 20 L8 13 L14 13 Z';
    p.appendChild(svg('path', { d: d, stroke: 'white', 'stroke-width': '3', 'stroke-linejoin': 'round', fill: 'white' }));
    p.appendChild(svg('path', { d: d, fill: '#111' }));
    plain.appendChild(p);
    root.appendChild(plain);
  }

  function armIdle() {
    clearTimeout(idleTimer);
    idleTimer = setTimeout(hide, IDLE_MS);
  }

  function show(label) {
    ensureRoot();
    if (!glow) buildGlow(label || 'Working in this tab');
    else if (wordsEl && label) wordsEl.textContent = label;
    requestAnimationFrame(function () {
      if (glow) glow.setAttribute('data-on', '');
    });
    armIdle();
  }

  function place(x, y) {
    cursorEl.style.transform = 'translate(' + x + 'px, ' + y + 'px)';
    plain.style.transform = 'translate3d(' + x + 'px, ' + y + 'px, 0)';
    ringEl.style.transform = 'translate(' + x + 'px, ' + y + 'px)';
  }

  function cursor(x, y, click, label) {
    ensureRoot();
    if (!glow) show();
    if (!layer) {
      buildCursor();
      // first appearance: start where the pointer lands, no flight across the page
      cursorEl.style.transition = 'none';
      plain.style.transition = 'none';
      place(x, y);
      void cursorEl.offsetWidth;
      cursorEl.style.transition = '';
      plain.style.transition = '';
    }
    layer.setAttribute('data-on', '');
    if (label !== undefined) {
      labelEl.textContent = label || '';
      labelEl.removeAttribute('data-spent');
      clearTimeout(spentTimer);
      spentTimer = setTimeout(function () {
        if (labelEl) labelEl.setAttribute('data-spent', '');
      }, 1000);
    }
    place(x, y);
    last = { x: x, y: y };
    if (click) {
      var ring = document.createElement('div');
      ringEl.replaceChildren(ring);
      setTimeout(function () {
        ring.remove();
      }, 650);
    }
    armIdle();
    return new Promise(function (resolve) {
      setTimeout(resolve, 260);
    });
  }

  // Screenshots: everything but a plain arrow disappears at once and comes back afterwards.
  function capture(on) {
    if (!host) return;
    var items = [glow, layer];
    for (var i = 0; i < items.length; i++) {
      if (!items[i]) continue;
      if (on) items[i].setAttribute('data-captured', '');
      else items[i].removeAttribute('data-captured');
    }
    if (plain) {
      if (on && layer) plain.setAttribute('data-on', '');
      else plain.removeAttribute('data-on');
    }
  }

  function hide() {
    clearTimeout(idleTimer);
    clearTimeout(spentTimer);
    if (glow) glow.removeAttribute('data-on');
    if (layer) layer.removeAttribute('data-on');
    if (plain) plain.removeAttribute('data-on');
    var gone = host;
    host = root = glow = layer = cursorEl = labelEl = ringEl = plain = wordsEl = null;
    setTimeout(function () {
      if (gone) gone.remove();
    }, 350);
  }

  window.addEventListener('beforeunload', function (e) {
    if (e.isTrusted) hide();
  });

  window.__klikUi = { version: 3, show: show, cursor: cursor, capture: capture, hide: hide };
})();
