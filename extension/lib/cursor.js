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
        0% { transform: scale(0.2); opacity: 1; }
        100% { transform: scale(2.2); opacity: 0; }
      }
      .browseruse-ripple-effect {
        position: fixed;
        width: 34px;
        height: 34px;
        margin-left: -17px;
        margin-top: -17px;
        border-radius: 50%;
        border: 2px solid #ff3b30;
        background: rgba(255, 59, 48, 0.35);
        pointer-events: none;
        z-index: 2147483646;
        animation: browseruse-click-ripple 0.45s cubic-bezier(0.1, 0.8, 0.3, 1) forwards;
      }
    `;
    document.documentElement.appendChild(styleEl);
  }

  let cursor = document.getElementById('browseruse-virtual-cursor');
  if (!cursor) {
    cursor = document.createElement('div');
    cursor.id = 'browseruse-virtual-cursor';
    cursor.style.cssText = `
      position: fixed;
      top: 0;
      left: 0;
      z-index: 2147483647;
      pointer-events: none;
      transition: transform 0.22s cubic-bezier(0.22, 1, 0.36, 1);
      will-change: transform;
      transform: translate3d(${x}px, ${y}px, 0);
    `;
    cursor.innerHTML = `
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" style="filter: drop-shadow(0 2px 5px rgba(0,0,0,0.45)); transform: translate(-2px, -2px);">
        <path d="M5.5 3.5L11.5 20.5L14.5 13.5L21.5 10.5L5.5 3.5Z" fill="#ff3b30" stroke="#ffffff" stroke-width="1.5" stroke-linejoin="round"/>
        <circle cx="5.5" cy="3.5" r="2.5" fill="#ffffff" />
      </svg>
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
    }, 500);
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
