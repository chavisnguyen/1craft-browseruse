/**
 * accessibility_tree.js - Full ARIA Accessibility Tree & Perception Engine.
 * Extracts accessible element hierarchies, assigns deterministic references (@e1, @e2...),
 * and automatically redacts sensitive fields (passwords, credit cards, OTPs).
 */

export function generateAccessibilityTreeFn(filter = 'interactive') {
  if (!window.__claudeElementMap) window.__claudeElementMap = {};
  if (!window.__claudeElementReverseMap) window.__claudeElementReverseMap = new WeakMap();

  // Reset old references
  const oldRefs = document.querySelectorAll('[data-browseruse-ref]');
  for (const el of oldRefs) {
    el.removeAttribute('data-browseruse-ref');
  }
  window.__claudeElementMap = {};

  const items = [];
  let refCount = 1;

  function isSensitiveField(el) {
    const type = (el.getAttribute('type') || '').toLowerCase();
    if (type === 'password' || type === 'hidden') return true;

    const autocomplete = (el.getAttribute('autocomplete') || '').toLowerCase();
    const sensitiveAutocompletes = [
      'current-password',
      'new-password',
      'one-time-code',
      'cc-number',
      'cc-csc',
      'cc-exp',
      'cc-exp-month',
      'cc-exp-year',
    ];
    for (const pattern of sensitiveAutocompletes) {
      if (autocomplete.includes(pattern)) return true;
    }

    const nameOrId = (el.name + ' ' + el.id).toLowerCase();
    if (nameOrId.includes('cvv') || nameOrId.includes('otp') || nameOrId.includes('secret')) {
      return true;
    }

    try {
      const style = window.getComputedStyle(el);
      if (style.webkitTextSecurity && style.webkitTextSecurity !== 'none') return true;
    } catch {}

    return false;
  }

  function getAriaRole(el) {
    const explicit = el.getAttribute('role');
    if (explicit) return explicit.toLowerCase();

    const tag = el.tagName.toLowerCase();
    const type = (el.getAttribute('type') || '').toLowerCase();

    switch (tag) {
      case 'a':
        return el.hasAttribute('href') ? 'link' : 'generic';
      case 'button':
        return 'button';
      case 'input':
        if (type === 'button' || type === 'submit' || type === 'reset') return 'button';
        if (type === 'checkbox') return 'checkbox';
        if (type === 'radio') return 'radio';
        if (type === 'file') return 'file_upload';
        return 'textbox';
      case 'textarea':
        return 'textbox';
      case 'select':
        return 'combobox';
      case 'h1':
      case 'h2':
      case 'h3':
      case 'h4':
      case 'h5':
      case 'h6':
        return 'heading';
      case 'nav':
        return 'navigation';
      case 'main':
        return 'main';
      case 'dialog':
        return 'dialog';
      default:
        return 'generic';
    }
  }

  function getAccessibleName(el) {
    const ariaLabel = el.getAttribute('aria-label');
    if (ariaLabel && ariaLabel.trim()) return ariaLabel.trim();

    const ariaLabelledBy = el.getAttribute('aria-labelledby');
    if (ariaLabelledBy) {
      const labelNode = document.getElementById(ariaLabelledBy);
      if (labelNode && labelNode.innerText) return labelNode.innerText.trim();
    }

    if (el.id) {
      const labelFor = document.querySelector(`label[for="${CSS.escape(el.id)}"]`);
      if (labelFor && labelFor.innerText) return labelFor.innerText.trim();
    }

    if (el.placeholder && el.placeholder.trim()) {
      return el.placeholder.trim();
    }

    // Direct text content (shallow or button text)
    let text = (el.innerText || el.value || '').trim();
    return text.replace(/\s+/g, ' ');
  }

  const querySelectorAll = (filter === 'all')
    ? 'a, button, input, textarea, select, [role], [tabindex], h1, h2, h3, h4, [onclick]'
    : 'a, button, input, textarea, select, [role="button"], [role="link"], [role="checkbox"], [role="radio"], [role="switch"], [role="tab"], [role="menuitem"], [role="combobox"], [role="dialog"], h1, h2, h3';

  const candidates = document.querySelectorAll(querySelectorAll);

  for (const el of candidates) {
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) continue;

    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') {
      continue;
    }

    const role = getAriaRole(el);
    const tag = el.tagName.toLowerCase();
    const isInteractive = (
      role === 'button' ||
      role === 'link' ||
      role === 'textbox' ||
      role === 'checkbox' ||
      role === 'radio' ||
      role === 'combobox' ||
      role === 'file_upload' ||
      role === 'switch' ||
      role === 'tab' ||
      role === 'menuitem' ||
      tag === 'input' ||
      tag === 'textarea' ||
      tag === 'select'
    );

    let accessibleName = getAccessibleName(el);

    // Security: Auto-redact sensitive inputs (passwords, credit cards, OTPs)
    if (isSensitiveField(el)) {
      accessibleName = '[REDACTED]';
    }

    // Skip generic non-interactive elements with no text
    if (!isInteractive && !accessibleName && !role.startsWith('heading')) {
      continue;
    }

    const ref = 'e' + refCount;
    el.setAttribute('data-browseruse-ref', ref);
    window.__claudeElementMap[ref] = el;
    window.__claudeElementReverseMap.set(el, ref);
    refCount++;

    // Extract rich state metadata
    const states = [];
    if (el.disabled || el.getAttribute('aria-disabled') === 'true') states.push('disabled');
    if (el.checked || el.getAttribute('aria-checked') === 'true') states.push('checked');
    if (el.getAttribute('aria-expanded') === 'true') states.push('expanded');
    if (el.getAttribute('aria-expanded') === 'false') states.push('collapsed');
    if (el.getAttribute('aria-selected') === 'true') states.push('selected');
    if (document.activeElement === el) states.push('focused');
    if (el.required || el.getAttribute('aria-required') === 'true') states.push('required');
    if (el.readOnly || el.getAttribute('aria-readonly') === 'true') states.push('readonly');

    const stateStr = states.length > 0 ? ` (${states.join(', ')})` : '';

    let meta = [];
    if (el.id) meta.push(`id="${el.id}"`);
    if (el.name) meta.push(`name="${el.name}"`);
    if (el.type && tag === 'input') meta.push(`type="${el.type}"`);
    const metaStr = meta.length > 0 ? ` [${meta.join(' ')}]` : '';

    const truncName = accessibleName.length > 80 ? accessibleName.slice(0, 77) + '...' : accessibleName;
    items.push(`@${ref} [${role}] "${truncName}"${stateStr}${metaStr}`);
  }

  return {
    url: window.location.href,
    title: document.title,
    viewport: {
      width: window.innerWidth,
      height: window.innerHeight,
      devicePixelRatio: window.devicePixelRatio || 1,
    },
    count: items.length,
    elements: items.join('\n'),
  };
}
