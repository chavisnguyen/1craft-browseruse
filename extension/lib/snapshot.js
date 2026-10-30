/**
 * jsExtractElements extracts up to 100 interactive elements in the page
 * matching the exact format from rodDriver.
 */
export function snapshotFn() {
  const items = [];
  const elements = document.querySelectorAll(
    'a, button, input, textarea, select, [role="button"], [role="link"], h1, h2, h3'
  );
  elements.forEach((el, idx) => {
    if (idx > 100) return;
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return;
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
    if (text || tag === 'input' || tag === 'textarea') {
      let meta = [];
      if (el.id) meta.push('id="' + el.id + '"');
      if (el.name) meta.push('name="' + el.name + '"');
      if (el.type) meta.push('type="' + el.type + '"');
      const metaStr = meta.length > 0 ? ' (' + meta.join(' ') + ')' : '';
      items.push('- [' + role + '] "' + text + '"' + metaStr);
    }
  });
  return items.join('\n');
}
