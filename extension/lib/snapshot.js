/**
 * snapshotFn extracts interactive elements in the page,
 * assigns deterministic data-browseruse-ref="e1", "e2"... attributes to each element,
 * and returns the elements list in @e1 [role] "Text" format.
 */
export function snapshotFn() {
  const oldRefs = document.querySelectorAll('[data-browseruse-ref]');
  for (const el of oldRefs) {
    el.removeAttribute('data-browseruse-ref');
  }

  const items = [];
  const elements = document.querySelectorAll(
    'a, button, input, textarea, select, [role="button"], [role="link"], h1, h2, h3'
  );
  let refCount = 1;
  for (const el of elements) {
    if (items.length >= 80) break;
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) continue;
    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden') continue;

    const text = (
      el.innerText ||
      el.value ||
      el.placeholder ||
      el.getAttribute('aria-label') ||
      ''
    )
      .trim()
      .replace(/\s+/g, ' ');
    const tag = el.tagName.toLowerCase();
    const role = el.getAttribute('role') || tag;
    if (text || tag === 'input' || tag === 'textarea' || tag === 'select') {
      const ref = 'e' + refCount;
      el.setAttribute('data-browseruse-ref', ref);
      refCount++;

      let meta = [];
      if (el.id) meta.push('id="' + el.id + '"');
      if (el.name) meta.push('name="' + el.name + '"');
      if (el.type) meta.push('type="' + el.type + '"');
      if (el.placeholder && el.placeholder !== text) meta.push('placeholder="' + el.placeholder + '"');
      const metaStr = meta.length > 0 ? ' (' + meta.join(' ') + ')' : '';
      const truncText = text.length > 80 ? text.substring(0, 77) + '...' : text;
      items.push('@' + ref + ' [' + role + '] "' + truncText + '"' + metaStr);
    }
  }
  return items.join('\n');
}
