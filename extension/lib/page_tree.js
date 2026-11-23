/**
 * page_tree.js - classic script injected on demand (chrome.scripting.executeScript({ files })).
 *
 * Builds the text accessibility tree used by read_page and assigns stable element
 * references ("ref_1", "ref_2", ...). References are kept in a WeakRef map so they
 * survive across tool calls but never keep removed DOM nodes alive.
 *
 * window.__klikRefMap      ref -> WeakRef(element)
 * window.__klikRefReverse  WeakMap(element -> ref)
 * window.__klikGenerateTree(filter, depth, maxChars, refId, opts)
 * window.__klikResolveRef(ref) -> { success, coordinates, hasBox, view } | { success:false, error }
 */
(function () {
  if (!window.__klikRefMap) window.__klikRefMap = {};
  if (!window.__klikRefReverse) window.__klikRefReverse = new WeakMap();
  if (!window.__klikRefCounter) window.__klikRefCounter = 0;

  var SKIP_TAGS = { script: 1, style: 1, meta: 1, link: 1, title: 1, noscript: 1, template: 1 };
  var INTERACTIVE_TAGS = { a: 1, button: 1, input: 1, select: 1, textarea: 1, details: 1, summary: 1 };
  var LANDMARK_TAGS = {
    h1: 1, h2: 1, h3: 1, h4: 1, h5: 1, h6: 1, nav: 1, main: 1, header: 1, footer: 1, section: 1, article: 1, aside: 1,
  };
  var TAG_ROLES = {
    a: 'link', button: 'button', select: 'combobox', textarea: 'textbox',
    h1: 'heading', h2: 'heading', h3: 'heading', h4: 'heading', h5: 'heading', h6: 'heading',
    img: 'image', nav: 'navigation', main: 'main', header: 'banner', footer: 'contentinfo',
    section: 'region', article: 'article', aside: 'complementary', form: 'form', table: 'table',
    ul: 'list', ol: 'list', li: 'listitem', label: 'label',
  };
  var SENSITIVE_AUTOCOMPLETE = [
    'current-password', 'new-password', 'one-time-code', 'cc-number', 'cc-csc', 'cc-exp', 'cc-exp-month', 'cc-exp-year',
  ];
  var MAX_ELEMENTS = 10000;

  function has(map, key) {
    return Object.prototype.hasOwnProperty.call(map, key);
  }

  function quote(text) {
    return '"' + String(text).replace(/[\s\u0000-\u001f\u0085]+/g, ' ').replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"';
  }

  function roleOf(el) {
    var explicit = (el.getAttribute('role') || '').trim().split(/\s+/)[0].replace(/[^A-Za-z-]/g, '');
    if (explicit) return explicit;
    var tag = el.tagName.toLowerCase();
    if (tag === 'input') {
      var type = el.getAttribute('type');
      if (type === 'submit' || type === 'button' || type === 'file') return 'button';
      if (type === 'checkbox') return 'checkbox';
      if (type === 'radio') return 'radio';
      return 'textbox';
    }
    return has(TAG_ROLES, tag) ? TAG_ROLES[tag] : 'generic';
  }

  function isSensitive(el) {
    var type = (el.getAttribute('type') || '').toLowerCase();
    if (type === 'password' || type === 'hidden') return true;
    var auto = (el.getAttribute('autocomplete') || '').toLowerCase();
    for (var i = 0; i < SENSITIVE_AUTOCOMPLETE.length; i++) {
      if (auto.indexOf(SENSITIVE_AUTOCOMPLETE[i]) !== -1) return true;
    }
    return false;
  }

  function ownText(el) {
    var out = '';
    for (var i = 0; i < el.childNodes.length; i++) {
      if (el.childNodes[i].nodeType === Node.TEXT_NODE) out += el.childNodes[i].textContent;
    }
    return out.trim();
  }

  function labelFor(id) {
    try {
      return document.querySelector('label[for="' + id.replace(/["\\]/g, '\\$&') + '"]');
    } catch (e) {
      return null;
    }
  }

  function attr(el, name) {
    var v = el.getAttribute(name);
    return v && v.trim() ? v.trim() : '';
  }

  function nameOf(el) {
    var tag = el.tagName.toLowerCase();
    if (tag === 'select') {
      if (isSensitive(el)) {
        return attr(el, 'aria-label') || attr(el, 'title') || '[value redacted]';
      }
      var selected = el.querySelector('option[selected]') || el.options[el.selectedIndex];
      if (selected && selected.textContent) return selected.textContent.trim();
    }
    var label = attr(el, 'aria-label') || attr(el, 'placeholder') || attr(el, 'title') || attr(el, 'alt');
    if (label) return label;
    if (el.id) {
      var lf = labelFor(el.id);
      if (lf && ownText(lf)) return ownText(lf);
    }
    if (tag === 'input') {
      var type = el.getAttribute('type') || '';
      var value = el.getAttribute('value');
      if (type === 'submit' && value && value.trim()) return value.trim();
      if (isSensitive(el)) return el.value ? '[value redacted]' : '';
      if (el.value && el.value.length < 50 && el.value.trim()) return el.value.trim();
    }
    if (tag === 'textarea' && isSensitive(el)) return el.value ? '[value redacted]' : '';
    if (tag === 'button' || tag === 'a' || tag === 'summary') {
      var t = ownText(el);
      if (t) return t;
    }
    if (/^h[1-6]$/.test(tag)) {
      var h = el.textContent;
      if (h && h.trim()) return h.trim().substring(0, 100);
    }
    if (tag === 'img') return '';
    var own = ownText(el);
    if (own && own.length >= 3) return own.length > 100 ? own.substring(0, 100) + '...' : own;
    return '';
  }

  function isVisible(el) {
    var style = window.getComputedStyle(el);
    return style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0' &&
      el.offsetWidth > 0 && el.offsetHeight > 0;
  }

  function inViewport(el) {
    var r = el.getBoundingClientRect();
    return r.top < window.innerHeight && r.bottom > 0 && r.left < window.innerWidth && r.right > 0;
  }

  function isInteractive(el) {
    var tag = el.tagName.toLowerCase();
    return has(INTERACTIVE_TAGS, tag) || el.getAttribute('onclick') !== null || el.getAttribute('tabindex') !== null ||
      el.getAttribute('role') === 'button' || el.getAttribute('role') === 'link' ||
      el.getAttribute('contenteditable') === 'true';
  }

  function shouldInclude(el, opts) {
    var tag = el.tagName.toLowerCase();
    if (has(SKIP_TAGS, tag)) return false;
    if (opts.filter !== 'all') {
      if (el.getAttribute('aria-hidden') === 'true') return false;
      if (!isVisible(el)) return false;
      if (!opts.refId && !inViewport(el)) return false;
    }
    if (opts.filter === 'interactive') return isInteractive(el);
    if (isInteractive(el)) return true;
    if (has(LANDMARK_TAGS, tag) || el.getAttribute('role') !== null) return true;
    if (nameOf(el).length > 0) return true;
    var role = roleOf(el);
    return role !== 'generic' && role !== 'image';
  }

  function refFor(el) {
    var ref = window.__klikRefReverse.get(el) || null;
    if (ref) {
      var existing = window.__klikRefMap[ref];
      if (!existing || existing.deref() !== el) ref = null;
    }
    if (!ref) {
      ref = 'ref_' + ++window.__klikRefCounter;
      window.__klikRefMap[ref] = new WeakRef(el);
      window.__klikRefReverse.set(el, ref);
    }
    return ref;
  }


  // ---- metadata used by the `find` tool (local matcher) ----
  function clip(text, n) {
    if (text == null) return '';
    text = String(text);
    if (text.length > 4 * n) text = text.substring(0, 4 * n);
    text = text.replace(/\s+/g, ' ').trim();
    return text.length > n ? text.substring(0, n) : text;
  }

  // Visible text of an element without descending into form controls, capped for speed.
  function shortText(el, limit, skipInteractiveChildren) {
    if (el.tagName.toLowerCase() in { script: 1, style: 1, template: 1, noscript: 1, textarea: 1, select: 1, option: 1, input: 1 }) return '';
    var out = '';
    var stack = [el];
    var visited = 0;
    while (stack.length && out.length < limit && visited < 300) {
      var node = stack.pop();
      visited++;
      if (node.nodeType === Node.TEXT_NODE) {
        var t = clip(node.nodeValue, limit);
        if (t) out += ' ' + t;
        continue;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) continue;
      if (node !== el) {
        var tg = node.tagName.toLowerCase();
        if (tg in { script: 1, style: 1, template: 1, noscript: 1, textarea: 1, select: 1, option: 1, input: 1 }) continue;
        if (skipInteractiveChildren && isInteractive(node)) continue;
        var aria = node.getAttribute('aria-label');
        if (aria) { out += ' ' + aria.substring(0, limit); continue; }
        if (tg === 'img') { out += ' ' + (node.getAttribute('alt') || '').substring(0, limit); continue; }
      }
      for (var i = Math.min(node.childNodes.length, 50) - 1; i >= 0; i--) stack.push(node.childNodes[i]);
    }
    return clip(out, limit);
  }

  var CONTAINER_TAGS = { tr: 1, li: 1, article: 1, dialog: 1, fieldset: 1, form: 1, aside: 1, section: 1 };
  var CONTAINER_ROLES = { row: 1, listitem: 1, article: 1, dialog: 1, alertdialog: 1 };

  // Text of the closest "card"-like ancestor, so repeated buttons can be told apart.
  function containerText(el) {
    var cur = el;
    var candidate = null;
    var seenMany = false;
    for (var i = 0; i < 6; i++) {
      var parent = cur.parentElement;
      if (!parent || parent === document.body) break;
      if (has(CONTAINER_TAGS, parent.tagName.toLowerCase()) || has(CONTAINER_ROLES, parent.getAttribute('role'))) {
        candidate = parent;
        break;
      }
      seenMany = seenMany || parent.getElementsByTagName(el.tagName).length > 3;
      if (!seenMany) candidate = parent;
      cur = parent;
    }
    return candidate ? shortText(candidate, 200, false) : '';
  }

  function describedBy(ids, limit) {
    var parts = [];
    var list = String(ids).split(/\s+/).slice(0, 3);
    for (var i = 0; i < list.length; i++) {
      var target = list[i] ? document.getElementById(list[i]) : null;
      if (target) parts.push(shortText(target, limit, false));
    }
    return clip(parts.join(' '), limit);
  }

  function nodeMeta(el, role) {
    var tag = el.tagName.toLowerCase();
    var rect = el.getBoundingClientRect();
    var visible = typeof el.checkVisibility === 'function'
      ? el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })
      : isVisible(el);
    var texts = {};
    var aria = el.getAttribute('aria-label');
    if (aria) texts.aria = clip(aria, 120);
    var labelParts = [];
    var labelledBy = el.getAttribute('aria-labelledby');
    if (labelledBy) labelParts.push(describedBy(labelledBy, 120));
    if (el.labels && el.labels.length) {
      for (var i = 0; i < el.labels.length && i < 3; i++) labelParts.push(shortText(el.labels[i], 120, false));
    }
    if (labelParts.length) texts.label = clip(labelParts.join(' '), 160);
    var placeholder = el.getAttribute('placeholder');
    if (placeholder) texts.placeholder = clip(placeholder, 120);
    var title = el.getAttribute('title');
    if (title) texts.title = clip(title, 120);
    var alt = el.getAttribute('alt');
    if (alt) texts.alt = clip(alt, 120);
    var type = (el.getAttribute('type') || '').toLowerCase();
    if (tag === 'input' && (type === 'submit' || type === 'button' || type === 'reset') && !isSensitive(el)) {
      var val = el.getAttribute('value');
      if (val) texts.value = clip(val, 120);
    }
    if (isInteractive(el) && tag !== 'input' && tag !== 'select' && tag !== 'textarea') {
      var text = shortText(el, 150, true);
      if (text) texts.text = text;
    }
    if (isInteractive(el) || role === 'image') {
      var container = containerText(el);
      if (container) texts.container = container;
    }
    var ids = [];
    var idAttrs = ['id', 'name', 'data-testid', 'data-test-id', 'data-test', 'data-qa', 'data-cy'];
    for (var j = 0; j < idAttrs.length; j++) {
      var v = el.getAttribute(idAttrs[j]);
      if (v) ids.push(clip(v, 60));
    }
    if (ids.length) texts.ids = clip(ids.join(' '), 160);
    var described = el.getAttribute('aria-describedby');
    if (described) texts.describedby = describedBy(described, 120);
    return {
      visible: !!visible && rect.width > 0 && rect.height > 0,
      inViewport: rect.top < window.innerHeight && rect.bottom > 0 && rect.left < window.innerWidth && rect.right > 0,
      disabled: el.disabled === true || el.getAttribute('aria-disabled') === 'true',
      texts: texts,
    };
  }

  window.__klikGenerateTree = function (filter, depthLimit, maxChars, refId, extra) {
    var lines = [];
    var maxDepth = depthLimit != null ? depthLimit : 15;
    var opts = { filter: filter || 'all', refId: refId || null };
    var count = 0;
    var findNodes = extra && extra.findData ? [] : null;
    var metaDeadline = Date.now() + 1500;
    var metaPartial = false;
    var viewport = { width: window.innerWidth, height: window.innerHeight };

    function walk(el, depth) {
      if (count >= MAX_ELEMENTS || depth > maxDepth || !el || !el.tagName) return;
      // aria-hidden subtrees are never exposed unless the caller asked for every element
      if (opts.filter !== 'all' && el.getAttribute('aria-hidden') === 'true') return;
      var included = shouldInclude(el, opts) || (opts.refId !== null && depth === 0);
      var tag = el.tagName.toLowerCase();
      if (included) {
        var role = roleOf(el);
        var name = nameOf(el).replace(/\s+/g, ' ').substring(0, 100);
        var ref = refFor(el);
        count++;
        var line = ' '.repeat(depth) + role;
        if (name) line += ' ' + quote(name);
        line += ' [' + ref + ']';
        if (el.getAttribute('href')) line += ' href=' + quote(el.getAttribute('href'));
        if (el.getAttribute('type')) line += ' type=' + quote(el.getAttribute('type'));
        if (el.getAttribute('placeholder')) line += ' placeholder=' + quote(el.getAttribute('placeholder'));
        lines.push(line);
        if (findNodes) {
          var node = { ref: ref, depth: depth, role: role, name: name };
          var hrefAttr = el.getAttribute('href');
          if (hrefAttr) node.href = clip(hrefAttr, 200);
          var ph = el.getAttribute('placeholder');
          if (ph) node.placeholder = clip(ph, 120);
          if (tag === 'select' && !isSensitive(el)) {
            node.options = [];
            for (var oi = 0; oi < el.options.length && oi < 300; oi++) {
              var ot = clip(el.options[oi].textContent, 100);
              if (ot) node.options.push(ot);
            }
          }
          if (!metaPartial && Date.now() > metaDeadline) metaPartial = true;
          if (!metaPartial) node.meta = nodeMeta(el, role);
          findNodes.push(node);
        }
        if (tag === 'select' && !isSensitive(el)) {
          for (var i = 0; i < el.options.length; i++) {
            var opt = el.options[i];
            var text = opt.textContent ? opt.textContent.trim() : '';
            var oline = ' '.repeat(depth + 1) + 'option';
            if (text) oline += ' ' + quote(text.replace(/\s+/g, ' ').substring(0, 100));
            if (opt.selected) oline += ' (selected)';
            if (opt.value && opt.value !== text) oline += ' value=' + quote(opt.value);
            lines.push(oline);
          }
        }
      }
      if ((tag !== 'select' || !isSensitive(el)) && el.children && depth < maxDepth) {
        for (var c = 0; c < el.children.length; c++) walk(el.children[c], included ? depth + 1 : depth);
      }
    }

    function fail(message) {
      return { error: message, pageContent: '', viewport: viewport };
    }

    if (opts.refId) {
      var weak = window.__klikRefMap[opts.refId];
      if (!weak) {
        return fail("Element with ref_id '" + opts.refId + "' not found. It may have been removed from the page. Use read_page without ref_id to get the current page state.");
      }
      var target = weak.deref();
      if (!target) {
        return fail("Element with ref_id '" + opts.refId + "' no longer exists. It may have been removed from the page. Use read_page without ref_id to get the current page state.");
      }
      walk(target, 0);
    } else if (document.body) {
      walk(document.body, 0);
    }

    for (var key in window.__klikRefMap) {
      if (!window.__klikRefMap[key].deref()) delete window.__klikRefMap[key];
    }

    var text = lines.join('\n');
    if (count >= MAX_ELEMENTS) {
      text += '\n[truncated at ' + MAX_ELEMENTS + ' elements — page is very large; use ref_id or a smaller depth to focus]';
    }
    if (maxChars != null && text.length > maxChars) {
      var total = text.length;
      var cut = text.lastIndexOf('\n', maxChars);
      if (cut <= 0) cut = Math.max(0, maxChars);
      text = text.slice(0, cut) +
        '\n[output truncated at ' + maxChars + ' of ' + total + ' characters. Pass a larger max_chars (default 50000) to see more, or use ref_id or a smaller depth to focus.]';
    }
    var result = { pageContent: text, viewport: viewport };
    if (findNodes) {
      result.findNodes = findNodes;
      result.findTruncated = count >= MAX_ELEMENTS;
      result.findMetaPartial = metaPartial;
    }
    return result;
  };

  // Scrolls the referenced element into view and returns its centre in viewport CSS pixels.
  window.__klikResolveRef = function (ref) {
    var snapshot = function () {
      return [location.href, window.scrollX, window.scrollY].join();
    };
    try {
      var weak = window.__klikRefMap[ref];
      var el = weak ? weak.deref() || null : null;
      var removed = false;
      if (el && !document.contains(el)) {
        delete window.__klikRefMap[ref];
        el = null;
        removed = true;
      }
      if (!el) {
        return {
          success: false,
          error: 'No element found with reference: "' + ref + '". The element may have been removed from the page.',
          view: removed ? snapshot() : undefined,
        };
      }
      el.scrollIntoView({ behavior: 'instant', block: 'center', inline: 'center' });
      if (el instanceof HTMLElement) void el.offsetHeight;
      var r = el.getBoundingClientRect();
      return {
        success: true,
        coordinates: [r.left + r.width / 2, r.top + r.height / 2],
        hasBox: el.getClientRects().length > 0,
        view: snapshot(),
      };
    } catch (e) {
      return { success: false, error: 'Error getting element coordinates: ' + (e && e.message ? e.message : 'Unknown error') };
    }
  };
})();
