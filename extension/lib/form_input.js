/**
 * form_input.js - sets a form control addressed by a ref_N from read_page.
 * Injected with chrome.scripting.executeScript({ func }), so it must be fully self-contained.
 * Resolves to { output } on success or { error, errorCode } on failure.
 */
export async function formInputInPage(ref, value) {
  const busy = (window.__klikFormInputBusy = window.__klikFormInputBusy || new WeakSet());

  // Clicks a checkbox/radio until `isDone()` holds (waits up to 750ms), like a user would.
  const clickUntil = async (el, noun, isDone, describeState) => {
    if (!isDone() && el.matches(':disabled')) {
      return { error: `The ${noun} is disabled, so it can't be changed.`, errorCode: 'input_invalid' };
    }
    if (busy.has(el)) {
      return {
        error: `Another form_input call is still changing this ${noun}. Take a screenshot to see its state before trying again.`,
        errorCode: 'input_invalid',
      };
    }
    let leaving = false;
    let wake = () => {};
    const onLeave = (e) => {
      if (e.isTrusted) {
        leaving = true;
        wake();
      }
    };
    addEventListener('beforeunload', onLeave);
    addEventListener('pagehide', onLeave);
    busy.add(el);
    try {
      el.focus();
      if (!isDone()) el.click();
      const deadline = performance.now() + 750;
      while (!isDone() && !leaving && el.isConnected && performance.now() < deadline) {
        await new Promise((resolve) => {
          wake = resolve;
          setTimeout(resolve, 50);
        });
      }
    } finally {
      busy.delete(el);
      removeEventListener('beforeunload', onLeave);
      removeEventListener('pagehide', onLeave);
    }
    if (!el.isConnected) {
      return {
        output: `The ${noun} is no longer on the page after the click. The page may have redrawn it, so read the page again to see the result.`,
      };
    }
    if (isDone()) return null;
    if (leaving) {
      return {
        error: `Clicked the ${noun}, but it was not in the wanted state when the page began to navigate away. Take a screenshot to see the result.`,
        errorCode: 'page_navigated_or_closed',
      };
    }
    return {
      error: `After the click, the ${noun} ${describeState()}. The page may have refused the change or may still be updating. Take a screenshot before trying again.`,
      errorCode: 'input_invalid',
    };
  };

  const fire = (el) => {
    el.focus();
    el.dispatchEvent(new Event('change', { bubbles: true }));
    el.dispatchEvent(new Event('input', { bubbles: true }));
  };

  try {
    let el = null;
    const weak = window.__klikRefMap && window.__klikRefMap[ref];
    if (weak) {
      el = weak.deref() || null;
      if (!el || !document.contains(el)) {
        delete window.__klikRefMap[ref];
        el = null;
      }
    }
    if (!el) {
      return { error: `No element found with reference: "${ref}". The element may have been removed from the page.`, errorCode: 'ref_stale' };
    }

    const rect = el.getBoundingClientRect();
    el.scrollIntoView({
      behavior: 'instant',
      block: rect.bottom <= 0 || rect.top >= innerHeight ? 'center' : 'nearest',
      inline: rect.right <= 0 || rect.left >= innerWidth ? 'center' : 'nearest',
    });

    if (el instanceof HTMLSelectElement) {
      const previous = el.value;
      const wanted = String(value);
      const options = Array.from(el.options);
      let found = false;
      for (let i = 0; i < options.length; i++) {
        if (options[i].value === wanted || options[i].text === wanted) {
          el.selectedIndex = i;
          found = true;
          break;
        }
      }
      if (!found) {
        const available = options.map((o) => `"${o.text}" (value: "${o.value}")`).join(', ');
        return { error: `Option "${wanted}" not found. Available options: ${available}`, errorCode: 'input_invalid' };
      }
      fire(el);
      return { output: `Selected option "${wanted}" in dropdown (previous: "${previous}")` };
    }

    if (el instanceof HTMLInputElement && el.type === 'checkbox') {
      const word = (on) => (on ? 'checked' : 'unchecked');
      const before = el.indeterminate ? 'mixed' : word(el.checked);
      if (typeof value !== 'boolean') return { error: 'Checkbox requires a boolean value (true/false)', errorCode: 'input_invalid' };
      if (el.indeterminate && el.checked === value) {
        return {
          error: `The checkbox is shown as mixed, and a click would make it ${word(!value)}. To make it ${word(value)}, set it to ${!value} first, then ${value}.`,
          errorCode: 'input_invalid',
        };
      }
      const failed = await clickUntil(
        el,
        'checkbox',
        () => el.checked === value && !el.indeterminate,
        () => `is ${el.indeterminate ? 'shown as mixed' : word(el.checked)}, not ${word(value)}`
      );
      return failed || { output: `Checkbox ${word(el.checked)} (previous: ${before})` };
    }

    if (el instanceof HTMLInputElement && el.type === 'radio') {
      const before = el.checked;
      const failed = await clickUntil(el, 'radio button', () => el.checked, () => 'is still not selected');
      return failed || { output: `Radio button selected (previous: ${before ? 'selected' : 'not selected'})` };
    }

    if (el instanceof HTMLInputElement && ['date', 'time', 'datetime-local', 'month', 'week'].includes(el.type)) {
      const previous = el.value;
      el.value = String(value);
      fire(el);
      return { output: `Set ${el.type} to "${el.value}" (previous: ${previous})` };
    }

    if (el instanceof HTMLInputElement && el.type === 'range') {
      const previous = el.value;
      const n = Number(value);
      if (isNaN(n)) return { error: 'Range input requires a numeric value', errorCode: 'input_invalid' };
      el.value = String(n);
      fire(el);
      return { output: `Set range to ${Number(el.value)} (previous: ${Number(previous)})` };
    }

    if (el instanceof HTMLInputElement && el.type === 'number') {
      const previous = el.value;
      if (isNaN(Number(value)) && value !== '') return { error: 'Number input requires a numeric value', errorCode: 'input_invalid' };
      el.value = String(value);
      fire(el);
      return { output: `Set number input to ${el.value} (previous: ${previous})` };
    }

    if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
      const previous = el.value;
      el.value = String(value);
      el.focus();
      if (el instanceof HTMLTextAreaElement || ['text', 'search', 'url', 'tel', 'password'].includes(el.type)) {
        el.setSelectionRange(el.value.length, el.value.length);
      }
      el.dispatchEvent(new Event('change', { bubbles: true }));
      el.dispatchEvent(new Event('input', { bubbles: true }));
      const kind = el instanceof HTMLTextAreaElement ? 'textarea' : el.type || 'text';
      return { output: `Set ${kind} value to "${el.value}" (previous: "${previous}")` };
    }

    return { error: `Element type "${el.tagName}" is not a supported form input`, errorCode: 'element_unsupported' };
  } catch (e) {
    return { error: `Error setting form value: ${e instanceof Error ? e.message : 'Unknown error'}`, errorCode: 'injected_script_failed' };
  }
}
