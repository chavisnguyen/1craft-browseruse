/**
 * cursor.js provides Virtual Cursor animations and Set-of-Marks (SoM) visual annotations.
 */

export function showVirtualCursorFn(x, y, isClick) {
  let styleEl = document.getElementById('browseruse-cursor-styles');
  if (!styleEl) {
    styleEl = document.createElement('style');
    styleEl.id = 'browseruse-cursor-styles';
    styleEl.textContent = `
      @keyframes browseruse-click-ripple {
        0% { transform: scale(0.2); opacity: 1; border-width: 4px; }
        50% { opacity: 0.85; }
        100% { transform: scale(2.8); opacity: 0; border-width: 1px; }
      }
      .browseruse-ripple-effect {
        position: fixed !important;
        width: 44px !important;
        height: 44px !important;
        margin-left: -22px !important;
        margin-top: -22px !important;
        border-radius: 50% !important;
        border: 3.5px solid #ff2a2a !important;
        background: radial-gradient(circle, rgba(255, 42, 42, 0.45) 0%, rgba(255, 42, 42, 0) 70%) !important;
        pointer-events: none !important;
        z-index: 2147483646 !important;
        animation: browseruse-click-ripple 0.6s cubic-bezier(0.1, 0.8, 0.3, 1) forwards !important;
      }
    `;
    document.documentElement.appendChild(styleEl);
  }

  let cursor = document.getElementById('browseruse-virtual-cursor');
  if (!cursor) {
    cursor = document.createElement('div');
    cursor.id = 'browseruse-virtual-cursor';
    cursor.style.cssText = `
      position: fixed !important;
      top: 0 !important;
      left: 0 !important;
      z-index: 2147483647 !important;
      pointer-events: none !important;
      transition: transform 0.24s cubic-bezier(0.22, 1, 0.36, 1) !important;
      will-change: transform !important;
      display: flex !important;
      align-items: center !important;
      gap: 6px !important;
      transform: translate3d(${x}px, ${y}px, 0) !important;
    `;
    cursor.innerHTML = `
      <svg width="32" height="32" viewBox="0 0 32 32" fill="none" style="filter: drop-shadow(0 0 8px rgba(255, 42, 42, 0.9)) drop-shadow(0 3px 6px rgba(0,0,0,0.6));">
        <path d="M5 3L14 26L18 17L27 13L5 3Z" fill="#ff2a2a" stroke="#ffffff" stroke-width="2.5" stroke-linejoin="round"/>
        <circle cx="6" cy="4" r="3" fill="#ffffff" />
      </svg>
      <span style="background: linear-gradient(135deg, #ff2a2a, #ff6b6b); color: #ffffff; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; font-size: 11px; font-weight: 700; padding: 2px 7px; border-radius: 6px; box-shadow: 0 2px 8px rgba(0,0,0,0.4); border: 1.5px solid #ffffff; letter-spacing: 0.3px; white-space: nowrap;">🤖 AI Agent</span>
    `;
    document.documentElement.appendChild(cursor);
  } else {
    cursor.style.transform = `translate3d(${x}px, ${y}px, 0)`;
  }

  if (isClick) {
    const ripple = document.createElement('div');
    ripple.className = 'browseruse-ripple-effect';
    ripple.style.left = `${x}px`;
    ripple.style.top = `${y}px`;
    document.documentElement.appendChild(ripple);
    setTimeout(() => {
      if (ripple.parentNode) ripple.parentNode.removeChild(ripple);
    }, 650);
  }
}

export function annotateElementsFn() {
  const overlayId = 'browseruse-som-overlay';
  const old = document.getElementById(overlayId);
  if (old) old.remove();

  const container = document.createElement('div');
  container.id = overlayId;
  container.style.cssText = 'position:fixed;top:0;left:0;width:100vw;height:100vh;pointer-events:none;z-index:2147483640;';

  const elements = document.querySelectorAll(
    'a, button, input, textarea, select, [role="button"], [role="link"], h1, h2, h3'
  );
  let count = 0;
  elements.forEach((el) => {
    if (count >= 100) return;
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return;
    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden') return;

    count++;
    // Box highlight
    const box = document.createElement('div');
    box.style.cssText = `position:absolute;left:${rect.left}px;top:${rect.top}px;width:${rect.width}px;height:${rect.height}px;border:2px solid #ff3b30;box-sizing:border-box;border-radius:3px;background:rgba(255, 59, 48, 0.08);`;

    // Numbered badge
    const badge = document.createElement('div');
    badge.innerText = `[${count}]`;
    badge.style.cssText = `position:absolute;left:${rect.left}px;top:${Math.max(0, rect.top - 18)}px;background:#ff3b30;color:#ffffff;font-size:11px;font-weight:bold;font-family:monospace;padding:1px 4px;border-radius:3px;box-shadow:0 1px 3px rgba(0,0,0,0.4);line-height:14px;`;

    container.appendChild(box);
    container.appendChild(badge);
  });
  document.documentElement.appendChild(container);
  return count;
}

export function removeAnnotateOverlayFn() {
  const el = document.getElementById('browseruse-som-overlay');
  if (el) el.remove();
}
