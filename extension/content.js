/**
 * browseruse content.js - Persistent Virtual Cursor
 * Ensures the virtual cursor is ALWAYS VISIBLE (visible toàn thời gian)
 * across all pages, resting at last known position and gliding on action.
 */

function injectPersistentCursor() {
  if (document.getElementById('browseruse-virtual-cursor')) return;

  let styleEl = document.getElementById('browseruse-cursor-styles');
  if (!styleEl) {
    styleEl = document.createElement('style');
    styleEl.id = 'browseruse-cursor-styles';
    styleEl.textContent = `
      @keyframes browseruse-click-ripple {
        0% { transform: scale(0.2); opacity: 1; border-width: 4px; }
        50% { opacity: 0.85; }
        100% { transform: scale(3.0); opacity: 0; border-width: 1px; }
      }
      .browseruse-ripple-effect {
        position: fixed !important;
        width: 44px !important;
        height: 44px !important;
        margin-left: -22px !important;
        margin-top: -22px !important;
        border-radius: 50% !important;
        border: 3px solid #ff2a2a !important;
        background: radial-gradient(circle, rgba(255, 42, 42, 0.45) 0%, rgba(255, 42, 42, 0) 70%) !important;
        pointer-events: none !important;
        z-index: 2147483646 !important;
        animation: browseruse-click-ripple 0.6s cubic-bezier(0.1, 0.8, 0.3, 1) forwards !important;
      }
      #browseruse-virtual-cursor {
        position: fixed !important;
        top: 0 !important;
        left: 0 !important;
        margin-left: -4px !important;
        margin-top: -2px !important;
        z-index: 2147483647 !important;
        pointer-events: none !important;
        user-select: none !important;
        will-change: transform !important;
        transition: transform 0.45s cubic-bezier(0.25, 1, 0.5, 1) !important;
        display: block !important;
        opacity: 1 !important;
        visibility: visible !important;
      }
    `;
    (document.head || document.documentElement).appendChild(styleEl);
  }

  const cursor = document.createElement('div');
  cursor.id = 'browseruse-virtual-cursor';
  cursor.innerHTML = `
    <svg width="28" height="28" viewBox="0 0 32 32" fill="none" style="filter: drop-shadow(0 0 6px rgba(255, 42, 42, 0.85)) drop-shadow(0 2px 5px rgba(0,0,0,0.5)); transform-origin: 5px 3px; transition: transform 0.12s ease-out;">
      <path d="M5 3L14 26L18 17L27 13L5 3Z" fill="#ff2a2a" stroke="#ffffff" stroke-width="2.5" stroke-linejoin="round"/>
      <circle cx="6" cy="4" r="3" fill="#ffffff" />
    </svg>
  `;

  // Start from last known coordinate or screen center
  let startX = Math.round(window.innerWidth / 2);
  let startY = Math.round(window.innerHeight / 2);
  try {
    const savedX = sessionStorage.getItem('__browseruse_cursor_x');
    const savedY = sessionStorage.getItem('__browseruse_cursor_y');
    if (savedX !== null && savedY !== null) {
      startX = parseInt(savedX, 10);
      startY = parseInt(savedY, 10);
    }
  } catch (e) {}

  cursor.style.setProperty('transform', `translate3d(${startX}px, ${startY}px, 0)`, 'important');
  (document.body || document.documentElement).appendChild(cursor);

  // Mouse move tracking
  window.addEventListener('mousemove', (e) => {
    if (!cursor) return;
    cursor.style.setProperty('transform', `translate3d(${e.clientX}px, ${e.clientY}px, 0)`, 'important');
    window.__browseruse_last_x = e.clientX;
    window.__browseruse_last_y = e.clientY;
    try {
      sessionStorage.setItem('__browseruse_cursor_x', e.clientX);
      sessionStorage.setItem('__browseruse_cursor_y', e.clientY);
    } catch (err) {}
  }, { capture: true, passive: true });
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', injectPersistentCursor);
} else {
  injectPersistentCursor();
}
