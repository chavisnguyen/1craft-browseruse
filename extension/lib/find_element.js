/**
 * find_element.js - Element locator, universal form input, and semantic element search.
 * Supports:
 * - Deterministic references (@e1, e1)
 * - XPath and CSS selectors
 * - Playwright-style :has-text("...")
 * - Button/Link text matching
 * - ARIA label, placeholder, name matching
 * - Deepest leaf text matching
 * - Form input: value filling, checkbox/radio toggling, select dropdowns
 * - Semantic element search (for the 'find' tool)
 */

export function findElementInPage(target, action, extraArg) {
  target = (target || '').trim();
  let clean = target.replace(/^["']|["']$/g, '');

  // Handle Playwright-style :has-text("...")
  const hasTextIdx = target.indexOf(':has-text(');
  if (hasTextIdx !== -1) {
    const start = hasTextIdx + ':has-text('.length;
    const end = target.lastIndexOf(')');
    if (end > start) {
      clean = target.slice(start, end).replace(/^["']|["']$/g, '').trim();
    }
  }

  function isVisible(el) {
    if (!el || !(el instanceof Element)) return false;
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return false;
    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden') return false;
    return true;
  }

  function findOnce() {
    // 0. Element reference (@e1, e1) via memory map or DOM attribute
    const refMatch = clean.match(/^@?e(\d+)$/i);
    if (refMatch) {
      const refKey = 'e' + refMatch[1];
      if (window.__claudeElementMap && window.__claudeElementMap[refKey]) {
        const memEl = window.__claudeElementMap[refKey];
        if (memEl && memEl.isConnected && isVisible(memEl)) {
          return memEl;
        }
      }
      try {
        const refEl = document.querySelector(`[data-browseruse-ref="${refKey}"]`);
        if (refEl && isVisible(refEl)) {
          return refEl;
        }
      } catch {}
    }

    // 1. XPath if starts with / or (
    if (target.startsWith('/') || target.startsWith('(')) {
      try {
        const xp = document.evaluate(
          target,
          document,
          null,
          XPathResult.FIRST_ORDERED_NODE_TYPE,
          null
        );
        const node = xp.singleNodeValue;
        if (node instanceof Element && isVisible(node)) {
          return node;
        }
      } catch {}
    }

    // 1b. Direct CSS selector
    try {
      const el = document.querySelector(target);
      if (el && isVisible(el)) {
        return el;
      }
    } catch {}

    // Compile regex for clean text
    let reg = null;
    try {
      reg = new RegExp(clean, 'i');
    } catch {}

    function textMatches(str) {
      if (!str) return false;
      if (reg) return reg.test(str);
      return str.toLowerCase().includes(clean.toLowerCase());
    }

    // 2. Button, a, [role='button'], [role='link']
    const interactive = document.querySelectorAll(
      "button, a, [role='button'], [role='link'], [role='tab'], [role='menuitem']"
    );
    for (const el of interactive) {
      if (!isVisible(el)) continue;
      const t = (el.innerText || el.textContent || '').trim();
      if (textMatches(t)) {
        return el;
      }
    }

    // 3. Label, placeholder, or input name match
    const escaped = clean.replace(/"/g, '\\"');
    const cssQuery = `[aria-label*="${escaped}"], [placeholder*="${escaped}"], input[name="${escaped}"]`;
    try {
      const matches = document.querySelectorAll(cssQuery);
      for (const el of matches) {
        if (isVisible(el)) {
          return el;
        }
      }
    } catch {}

    // 4. Any element with text (deepest element priority)
    const all = document.querySelectorAll('*');
    let bestEl = null;
    let maxDepth = -1;

    for (const el of all) {
      const tag = el.tagName;
      if (
        tag === 'SCRIPT' ||
        tag === 'STYLE' ||
        tag === 'NOSCRIPT' ||
        tag === 'META' ||
        tag === 'LINK'
      ) {
        continue;
      }
      if (!isVisible(el)) continue;

      const t = (el.innerText || el.textContent || '').trim();
      if (!textMatches(t)) continue;

      let childMatches = false;
      for (const child of el.children) {
        if (!isVisible(child)) continue;
        const ct = (child.innerText || child.textContent || '').trim();
        if (textMatches(ct)) {
          childMatches = true;
          break;
        }
      }

      if (!childMatches) {
        let depth = 0;
        let p = el.parentElement;
        while (p) {
          depth++;
          p = p.parentElement;
        }
        if (depth > maxDepth) {
          maxDepth = depth;
          bestEl = el;
        }
      }
    }

    if (bestEl) return bestEl;
    return null;
  }

  // Polling with retry up to 2 seconds
  return new Promise((resolve) => {
    const startTime = Date.now();

    function poll() {
      const el = findOnce();
      if (el) {
        el.scrollIntoView({ block: 'center', inline: 'center' });
        const rect = el.getBoundingClientRect();
        const center = {
          x: Math.round(rect.left + rect.width / 2),
          y: Math.round(rect.top + rect.height / 2),
          width: rect.width,
          height: rect.height,
        };

        if (action === 'prepare_click') {
          const clickable = el.closest('a, button, [role=button], input[type=submit], select') || el;
          clickable.__bu_clicked = false;
          const handler = () => { clickable.__bu_clicked = true; };
          clickable.addEventListener('click', handler, { capture: true, once: true });
          clickable.__bu_handler = handler;
          resolve({ found: true, center });
          return;
        }

        if (action === 'ensure_click') {
          const clickable = el.closest('a, button, [role=button], input[type=submit], select') || el;
          if (clickable.__bu_handler) {
            clickable.removeEventListener('click', clickable.__bu_handler, { capture: true });
            delete clickable.__bu_handler;
          }
          if (!clickable.__bu_clicked) {
            clickable.click();
          }
          delete clickable.__bu_clicked;
          resolve({ found: true, center });
          return;
        }

        if (action === 'click_fallback') {
          el.click();
          resolve({ found: true, center });
          return;
        }

        if (action === 'hover_fallback') {
          el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true, cancelable: true, clientX: center.x, clientY: center.y }));
          el.dispatchEvent(new MouseEvent('mouseenter', { bubbles: true, cancelable: true, clientX: center.x, clientY: center.y }));
          el.dispatchEvent(new MouseEvent('mousemove', { bubbles: true, cancelable: true, clientX: center.x, clientY: center.y }));
          resolve({ found: true, center });
          return;
        }

        if (action === 'form_input') {
          const tag = el.tagName.toLowerCase();
          const type = (el.getAttribute('type') || '').toLowerCase();
          const argObj = typeof extraArg === 'object' && extraArg !== null ? extraArg : { value: String(extraArg || '') };

          // 1. Checkbox or Radio
          if (type === 'checkbox' || type === 'radio' || el.getAttribute('role') === 'switch' || el.getAttribute('role') === 'checkbox') {
            const shouldBeChecked = argObj.checked !== undefined ? Boolean(argObj.checked) : !el.checked;
            if (el.checked !== shouldBeChecked) {
              el.checked = shouldBeChecked;
              if (el.setAttribute) el.setAttribute('aria-checked', String(shouldBeChecked));
              el.dispatchEvent(new Event('input', { bubbles: true }));
              el.dispatchEvent(new Event('change', { bubbles: true }));
              el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
            }
            resolve({ found: true, center, type: 'checkbox', checked: el.checked });
            return;
          }

          // 2. Select dropdown
          if (tag === 'select') {
            const val = String(argObj.value || '').toLowerCase().trim();
            let matchedOption = null;
            for (const opt of el.options) {
              if (opt.value.toLowerCase() === val || opt.text.toLowerCase().includes(val)) {
                matchedOption = opt;
                break;
              }
            }
            if (matchedOption) {
              el.value = matchedOption.value;
              matchedOption.selected = true;
              el.dispatchEvent(new Event('input', { bubbles: true }));
              el.dispatchEvent(new Event('change', { bubbles: true }));
              resolve({ found: true, center, type: 'select', selected: matchedOption.value });
              return;
            }
          }

          // 3. Text input, textarea, contenteditable
          el.focus();
          const textVal = String(argObj.value !== undefined ? argObj.value : '');
          if (tag === 'input' || tag === 'textarea') {
            el.value = textVal;
            el.dispatchEvent(new Event('input', { bubbles: true }));
            el.dispatchEvent(new Event('change', { bubbles: true }));
          } else if (el.isContentEditable) {
            el.innerText = textVal;
            el.dispatchEvent(new Event('input', { bubbles: true }));
          }
          resolve({ found: true, center, type: 'textbox', value: textVal });
          return;
        }

        if (action === 'select') {
          let selectEl = el;
          if (el.tagName !== 'SELECT') {
            selectEl = el.querySelector('select') || el.closest('select');
          }
          if (!selectEl || selectEl.tagName !== 'SELECT') {
            resolve({ found: false, error: `element matching '${target}' is not a <select> element` });
            return;
          }
          let matchedOption = null;
          const val = (extraArg || '').toLowerCase().trim();
          for (const opt of selectEl.options) {
            if (opt.value.toLowerCase() === val || opt.text.toLowerCase().includes(val)) {
              matchedOption = opt;
              break;
            }
          }
          if (!matchedOption) {
            resolve({ found: false, error: `no option found matching '${extraArg}' in <select>` });
            return;
          }
          selectEl.value = matchedOption.value;
          matchedOption.selected = true;
          selectEl.dispatchEvent(new Event('input', { bubbles: true }));
          selectEl.dispatchEvent(new Event('change', { bubbles: true }));
          resolve({ found: true, center, selectedValue: matchedOption.value, selectedText: matchedOption.text });
          return;
        }

        if (action === 'prepare_type') {
          el.focus();
          if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
            el.select();
          } else if (el.isContentEditable) {
            const sel = window.getSelection();
            sel.removeAllRanges();
            const range = document.createRange();
            range.selectNodeContents(el);
            sel.addRange(range);
          }
          resolve({ found: true, center, isInput: true });
          return;
        }

        if (action === 'type_fallback') {
          el.focus();
          if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
            el.value = extraArg || '';
            el.dispatchEvent(new Event('input', { bubbles: true }));
            el.dispatchEvent(new Event('change', { bubbles: true }));
          } else {
            el.innerText = extraArg || '';
          }
          resolve({ found: true, center });
          return;
        }

        // Default 'inspect' / 'find'
        resolve({ found: true, center });
        return;
      }

      if (Date.now() - startTime >= 2000) {
        resolve({
          found: false,
          error: `no element found matching '${target}'`,
        });
        return;
      }

      setTimeout(poll, 200);
    }

    poll();
  });
}

/**
 * Searches the page for elements matching a natural language query or text.
 */
export function findElementsQueryInPage(query) {
  const q = (query || '').toLowerCase().trim();
  if (!q) return [];

  const candidates = document.querySelectorAll('a, button, input, textarea, select, [role], h1, h2, h3, p, span');
  const matches = [];

  for (const el of candidates) {
    if (matches.length >= 15) break;
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) continue;
    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden') continue;

    const text = (el.innerText || el.value || el.getAttribute('aria-label') || el.placeholder || '').trim();
    if (text.toLowerCase().includes(q)) {
      const ref = el.getAttribute('data-browseruse-ref') || '';
      matches.push({
        ref: ref ? '@' + ref : '',
        tag: el.tagName.toLowerCase(),
        role: el.getAttribute('role') || el.tagName.toLowerCase(),
        text: text.slice(0, 80),
        center: {
          x: Math.round(rect.left + rect.width / 2),
          y: Math.round(rect.top + rect.height / 2),
        },
      });
    }
  }

  return matches;
}

export function waitForElementInPage(target, timeoutMs, state) {
  target = (target || '').trim();
  timeoutMs = timeoutMs > 0 ? timeoutMs : 5000;
  state = (state || 'visible').toLowerCase();

  return new Promise((resolve) => {
    const startTime = Date.now();

    function check() {
      let el = null;
      try {
        if (target.startsWith('/') || target.startsWith('(')) {
          const xp = document.evaluate(target, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
          el = xp.singleNodeValue;
        } else {
          el = document.querySelector(target);
        }
      } catch {}

      const isAttached = el instanceof Element;
      let isVis = false;
      if (isAttached) {
        const rect = el.getBoundingClientRect();
        const style = window.getComputedStyle(el);
        isVis = rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden';
      }

      if (state === 'attached' && isAttached) {
        resolve({ satisfied: true, state: 'attached' });
        return;
      }
      if (state === 'visible' && isVis) {
        resolve({ satisfied: true, state: 'visible' });
        return;
      }
      if (state === 'hidden' && !isVis) {
        resolve({ satisfied: true, state: 'hidden' });
        return;
      }

      if (Date.now() - startTime >= timeoutMs) {
        resolve({
          satisfied: false,
          error: `timed out waiting ${timeoutMs}ms for '${target}' to be ${state}`,
        });
        return;
      }

      setTimeout(check, 100);
    }

    check();
  });
}
