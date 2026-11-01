/**
 * find_element.js reproduces rod's findElement 4-strategy algorithm inside the browser page context.
 *
 * Strategies:
 * 1. Direct selector (CSS, XPath)
 * 2. Button, Link with matching text (including Playwright-style :has-text("..."))
 * 3. Label, placeholder, or input name match
 * 4. Any element with text (deepest leaf match priority)
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
      } catch {
        // ignore xpath error
      }
    }

    // 1b. Direct CSS selector
    try {
      const el = document.querySelector(target);
      if (el && isVisible(el)) {
        return el;
      }
    } catch {
      // ignore invalid selector
    }

    // Compile regex for clean text
    let reg = null;
    try {
      reg = new RegExp(clean, 'i');
    } catch {
      // fallback to substring
    }

    function textMatches(str) {
      if (!str) return false;
      if (reg) return reg.test(str);
      return str.toLowerCase().includes(clean.toLowerCase());
    }

    // 2. Button, a, [role='button'], [role='link']
    const interactive = document.querySelectorAll(
      "button, a, [role='button'], [role='link']"
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
    } catch {
      // ignore
    }

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

      // Check if any visible child also matches
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

  // Polling with retry up to 2 seconds (matching rod's 2s timeout)
  return new Promise((resolve, reject) => {
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
      } catch {
        // ignore selector error
      }

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

