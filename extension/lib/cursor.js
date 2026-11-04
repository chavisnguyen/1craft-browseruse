/**
 * cursor.js - Visual Indicator, Emergency Stop HUD, and Physics-based Cursor Engine.
 * Built to match Claude in Chrome's visual identity & safety standards.
 * Each function is self-contained so it can be cleanly serialized by chrome.scripting.executeScript.
 */

export function setAgentActiveState(isActive, actionLabel = 'OneCraft is interacting...') {
  function ensureVisualStyles() {
    let styleEl = document.getElementById('claude-agent-styles');
    if (styleEl) return;

    styleEl = document.createElement('style');
    styleEl.id = 'claude-agent-styles';
    styleEl.textContent = `
      @keyframes claude-pulse {
        0%, 100% { opacity: 0.65; }
        50% { opacity: 1; }
      }
      @keyframes claude-ring-expand {
        0% { transform: scale(0.2); opacity: 1; border-width: 3.5px; }
        60% { opacity: 0.85; }
        100% { transform: scale(2.8); opacity: 0; border-width: 1px; }
      }
      #claude-agent-glow-border {
        position: fixed !important;
        inset: 0 !important;
        pointer-events: none !important;
        z-index: 2147483645 !important;
        opacity: 0 !important;
        transition: opacity 0.35s ease-in-out !important;
      }
      #claude-agent-glow-border-inner {
        position: absolute !important;
        inset: 0 !important;
        will-change: opacity !important;
        box-shadow:
          inset 0 0 15px rgba(217, 119, 87, 0.75),
          inset 0 0 25px rgba(217, 119, 87, 0.5),
          inset 0 0 40px rgba(217, 119, 87, 0.25) !important;
        animation: claude-pulse 2s ease-in-out infinite !important;
      }
      #claude-agent-stop-container {
        position: fixed !important;
        bottom: 20px !important;
        left: 50% !important;
        transform: translateX(-50%) !important;
        display: inline-flex !important;
        align-items: center !important;
        gap: 10px !important;
        padding: 7px 14px 7px 16px !important;
        background: rgba(250, 249, 245, 0.96) !important;
        backdrop-filter: blur(10px) !important;
        border: 1px solid rgba(217, 119, 87, 0.4) !important;
        border-radius: 9999px !important;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.18), 0 2px 6px rgba(217, 119, 87, 0.15) !important;
        z-index: 2147483647 !important;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif !important;
        user-select: none !important;
        pointer-events: auto !important;
        opacity: 0 !important;
        transition: opacity 0.3s ease, transform 0.3s cubic-bezier(0.16, 1, 0.3, 1) !important;
      }
      #claude-agent-stop-container.active {
        opacity: 1 !important;
      }
      #claude-agent-status-dot {
        width: 8px !important;
        height: 8px !important;
        border-radius: 50% !important;
        background: #D97757 !important;
        box-shadow: 0 0 8px #D97757 !important;
        animation: claude-pulse 1.4s ease-in-out infinite !important;
      }
      #claude-agent-status-text {
        font-size: 13px !important;
        font-weight: 500 !important;
        color: #2D2B28 !important;
        max-width: 320px !important;
        overflow: hidden !important;
        text-overflow: ellipsis !important;
        white-space: nowrap !important;
      }
      #claude-agent-stop-button {
        display: inline-flex !important;
        align-items: center !important;
        gap: 4px !important;
        padding: 4px 10px !important;
        background: #D97757 !important;
        color: #FAF9F5 !important;
        border: none !important;
        border-radius: 12px !important;
        font-size: 12px !important;
        font-weight: 600 !important;
        cursor: pointer !important;
        transition: background 0.15s ease, transform 0.1s ease !important;
      }
      #claude-agent-stop-button:hover {
        background: #C46243 !important;
        transform: scale(1.03) !important;
      }
      #claude-agent-stop-button:active {
        transform: scale(0.96) !important;
      }
      #claude-phantom-cursor {
        position: fixed !important;
        top: 0 !important;
        left: 0 !important;
        pointer-events: none !important;
        z-index: 2147483646 !important;
        opacity: 0 !important;
        will-change: transform, opacity !important;
        transition: opacity 0.2s ease-out !important;
      }
      .claude-cursor-ring {
        position: fixed !important;
        width: 40px !important;
        height: 40px !important;
        margin-left: -20px !important;
        margin-top: -20px !important;
        border-radius: 50% !important;
        border: 3px solid #D97757 !important;
        background: radial-gradient(circle, rgba(217, 119, 87, 0.45) 0%, rgba(217, 119, 87, 0) 70%) !important;
        pointer-events: none !important;
        z-index: 2147483646 !important;
        animation: claude-ring-expand 0.6s cubic-bezier(0.1, 0.8, 0.3, 1) forwards !important;
      }
      #claude-cursor-label {
        position: absolute !important;
        left: 20px !important;
        top: 14px !important;
        padding: 3px 8px !important;
        background: #30302E !important;
        color: #FAF9F5 !important;
        border-radius: 6px !important;
        font-size: 11px !important;
        font-weight: 500 !important;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif !important;
        white-space: nowrap !important;
        box-shadow: 0 2px 6px rgba(0, 0, 0, 0.3) !important;
        pointer-events: none !important;
      }
    `;
    (document.head || document.documentElement).appendChild(styleEl);
  }

  ensureVisualStyles();

  let border = document.getElementById('claude-agent-glow-border');
  if (!border && isActive) {
    border = document.createElement('div');
    border.id = 'claude-agent-glow-border';
    border.setAttribute('aria-hidden', 'true');
    const inner = document.createElement('div');
    inner.id = 'claude-agent-glow-border-inner';
    border.appendChild(inner);
    (document.body || document.documentElement).appendChild(border);
  }

  let hud = document.getElementById('claude-agent-stop-container');
  if (!hud && isActive) {
    hud = document.createElement('div');
    hud.id = 'claude-agent-stop-container';
    hud.setAttribute('aria-hidden', 'true');
    hud.innerHTML = `
      <div id="claude-agent-status-dot"></div>
      <span id="claude-agent-status-text">${actionLabel}</span>
      <button id="claude-agent-stop-button" type="button" title="Stop OneCraft Agent">
        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor">
          <rect x="2" y="2" width="12" height="12" rx="2" />
        </svg>
        Stop
      </button>
    `;
    (document.body || document.documentElement).appendChild(hud);

    const stopBtn = hud.querySelector('#claude-agent-stop-button');
    if (stopBtn) {
      stopBtn.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
        window.__claude_agent_cancelled = true;
        hud.classList.remove('active');
        if (border) border.style.opacity = '0';
        try {
          chrome.runtime.sendMessage({ type: 'EMERGENCY_STOP' });
        } catch {}
      });
    }
  }

  if (isActive) {
    if (border) border.style.setProperty('opacity', '1', 'important');
    if (hud) {
      const textEl = hud.querySelector('#claude-agent-status-text');
      if (textEl && actionLabel) textEl.textContent = actionLabel;
      hud.classList.add('active');
    }
  } else {
    if (border) border.style.setProperty('opacity', '0', 'important');
    if (hud) hud.classList.remove('active');
    setTimeout(() => {
      if (!window.__claude_agent_active) {
        if (border) border.remove();
        if (hud) hud.remove();
      }
    }, 400);
  }
}

export function animateCursorTo(targetX, targetY, isClick = false, actionText = '') {
  function ensureVisualStyles() {
    let styleEl = document.getElementById('claude-agent-styles');
    if (styleEl) return;

    styleEl = document.createElement('style');
    styleEl.id = 'claude-agent-styles';
    styleEl.textContent = `
      @keyframes claude-pulse {
        0%, 100% { opacity: 0.65; }
        50% { opacity: 1; }
      }
      @keyframes claude-ring-expand {
        0% { transform: scale(0.2); opacity: 1; border-width: 3.5px; }
        60% { opacity: 0.85; }
        100% { transform: scale(2.8); opacity: 0; border-width: 1px; }
      }
      #claude-agent-glow-border {
        position: fixed !important;
        inset: 0 !important;
        pointer-events: none !important;
        z-index: 2147483645 !important;
        opacity: 0 !important;
        transition: opacity 0.35s ease-in-out !important;
      }
      #claude-agent-glow-border-inner {
        position: absolute !important;
        inset: 0 !important;
        will-change: opacity !important;
        box-shadow:
          inset 0 0 15px rgba(217, 119, 87, 0.75),
          inset 0 0 25px rgba(217, 119, 87, 0.5),
          inset 0 0 40px rgba(217, 119, 87, 0.25) !important;
        animation: claude-pulse 2s ease-in-out infinite !important;
      }
      #claude-phantom-cursor {
        position: fixed !important;
        top: 0 !important;
        left: 0 !important;
        pointer-events: none !important;
        z-index: 2147483646 !important;
        opacity: 0 !important;
        will-change: transform, opacity !important;
        transition: opacity 0.2s ease-out !important;
      }
      .claude-cursor-ring {
        position: fixed !important;
        width: 40px !important;
        height: 40px !important;
        margin-left: -20px !important;
        margin-top: -20px !important;
        border-radius: 50% !important;
        border: 3px solid #D97757 !important;
        background: radial-gradient(circle, rgba(217, 119, 87, 0.45) 0%, rgba(217, 119, 87, 0) 70%) !important;
        pointer-events: none !important;
        z-index: 2147483646 !important;
        animation: claude-ring-expand 0.6s cubic-bezier(0.1, 0.8, 0.3, 1) forwards !important;
      }
      #claude-cursor-label {
        position: absolute !important;
        left: 20px !important;
        top: 14px !important;
        padding: 3px 8px !important;
        background: #30302E !important;
        color: #FAF9F5 !important;
        border-radius: 6px !important;
        font-size: 11px !important;
        font-weight: 500 !important;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif !important;
        white-space: nowrap !important;
        box-shadow: 0 2px 6px rgba(0, 0, 0, 0.3) !important;
        pointer-events: none !important;
      }
    `;
    (document.head || document.documentElement).appendChild(styleEl);
  }

  function clamp(val, min, max) {
    return Math.max(min, Math.min(max, val));
  }

  function smoothStep(t) {
    if (t <= 0) return 0;
    if (t >= 1) return 1;
    return t * t * t * (10 - 15 * t + 6 * t * t);
  }

  function planTrip(start, end) {
    const tune = {
      baseMs: 80,
      perBitMs: 70,
      targetPx: 24,
      minMs: 140,
      maxMs: 380,
      releaseMs: 380,
      arc: 0.1,
      arcMaxPx: 30,
      elbow: { x: 0.6, y: 0.8 },
      overshoot: 0.03,
      overshootMaxPx: 4,
      mainEnds: 0.8,
      fixStarts: 0.66,
      steps: 16,
    };
    const dx = end.x - start.x;
    const dy = end.y - start.y;
    const dist = Math.max(Math.hypot(dx, dy), 1e-6);

    const rawDuration = tune.baseMs + tune.perBitMs * Math.log2(dist / tune.targetPx + 1);
    const durationMs = dist < 0.5 ? 0 : Math.round(clamp(rawDuration, tune.minMs, tune.maxMs));
    const overshootDist = Math.min(tune.overshoot * dist, tune.overshootMaxPx);
    const arcSign = (dx * tune.elbow.y - dy * tune.elbow.x > 0 ? -1 : 1);
    const arcDist = arcSign * Math.min(tune.arc * dist, tune.arcMaxPx);

    const points = [];
    for (let i = 0; i <= tune.steps; i++) {
      const s = i / tune.steps;
      const fixProgress = tune.fixStarts < 1 ? (s - tune.fixStarts) / (1 - tune.fixStarts) : 0;
      const fixAmount = overshootDist * smoothStep(fixProgress);
      const mainProgress = s / tune.mainEnds;
      const normDist = ((dist + overshootDist) * smoothStep(mainProgress) - fixAmount) / dist;
      const arcProgress = (arcDist * Math.sin(Math.PI * clamp(normDist, 0, 1))) / dist;

      points.push({
        x: start.x + dx * normDist - dy * arcProgress,
        y: start.y + dy * normDist + dx * arcProgress,
      });
    }

    return {
      durationMs,
      releaseMs: Math.min(durationMs, tune.releaseMs),
      points,
      keyframes: points.map((p) => ({
        transform: `translate3d(${p.x.toFixed(2)}px, ${p.y.toFixed(2)}px, 0)`,
      })),
    };
  }

  ensureVisualStyles();

  let cursor = document.getElementById('claude-phantom-cursor');
  if (!cursor) {
    cursor = document.createElement('div');
    cursor.id = 'claude-phantom-cursor';
    cursor.setAttribute('aria-hidden', 'true');
    cursor.innerHTML = `
      <svg width="24" height="28" viewBox="0 0 20 26" fill="none" style="filter: drop-shadow(0 0 4px rgba(217,119,87,0.9)) drop-shadow(0 2px 8px rgba(0,0,0,0.45)); transform-origin: 0 0; transition: transform 0.12s ease-out;">
        <path d="M0 0 L0 18 L4.5 14 L7.5 21.5 L11 20 L8 13 L14 13 Z" fill="#D97757" stroke="#FAF9F5" stroke-width="2" stroke-linejoin="round"/>
      </svg>
      <div id="claude-cursor-label" style="display: none;"></div>
    `;
    const initX = window.__claude_cursor_x !== undefined ? window.__claude_cursor_x : Math.round(window.innerWidth / 2);
    const initY = window.__claude_cursor_y !== undefined ? window.__claude_cursor_y : Math.round(window.innerHeight / 2);
    cursor.style.transform = `translate3d(${initX}px, ${initY}px, 0)`;
    (document.body || document.documentElement).appendChild(cursor);
  }

  const startX = window.__claude_cursor_x !== undefined ? window.__claude_cursor_x : targetX;
  const startY = window.__claude_cursor_y !== undefined ? window.__claude_cursor_y : targetY;
  const trip = planTrip({ x: startX, y: startY }, { x: targetX, y: targetY });

  cursor.style.setProperty('opacity', '1', 'important');
  const labelEl = cursor.querySelector('#claude-cursor-label');
  if (labelEl) {
    if (actionText) {
      labelEl.textContent = actionText;
      labelEl.style.display = 'block';
    } else {
      labelEl.style.display = 'none';
    }
  }

  const anim = cursor.animate(trip.keyframes, {
    duration: trip.durationMs,
    easing: 'linear',
    fill: 'forwards',
  });

  window.__claude_cursor_x = targetX;
  window.__claude_cursor_y = targetY;

  return new Promise((resolve) => {
    anim.onfinish = () => {
      cursor.style.transform = `translate3d(${targetX}px, ${targetY}px, 0)`;

      if (isClick) {
        const svg = cursor.querySelector('svg');
        if (svg) svg.style.transform = 'scale(0.85)';

        const ring = document.createElement('div');
        ring.className = 'claude-cursor-ring';
        ring.style.left = `${targetX}px`;
        ring.style.top = `${targetY}px`;
        (document.body || document.documentElement).appendChild(ring);

        setTimeout(() => {
          if (svg) svg.style.transform = 'scale(1)';
        }, 150);

        setTimeout(() => {
          ring.remove();
        }, 600);
      }

      if (window.__claude_fade_timer) clearTimeout(window.__claude_fade_timer);
      window.__claude_fade_timer = setTimeout(() => {
        if (cursor) cursor.style.setProperty('opacity', '0', 'important');
      }, 1400);

      resolve();
    };
  });
}

export function hideAgentIndicatorsForScreenshot() {
  const border = document.getElementById('claude-agent-glow-border');
  const hud = document.getElementById('claude-agent-stop-container');
  const cursor = document.getElementById('claude-phantom-cursor');
  if (border) border.style.setProperty('display', 'none', 'important');
  if (hud) hud.style.setProperty('display', 'none', 'important');
  if (cursor) cursor.style.setProperty('display', 'none', 'important');
}

export function showAgentIndicatorsAfterScreenshot() {
  const border = document.getElementById('claude-agent-glow-border');
  const hud = document.getElementById('claude-agent-stop-container');
  const cursor = document.getElementById('claude-phantom-cursor');
  if (border) border.style.removeProperty('display');
  if (hud) hud.style.removeProperty('display');
  if (cursor) cursor.style.removeProperty('display');
}
