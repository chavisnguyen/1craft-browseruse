import { ToolError } from './errors.js';

// Chrome DevTools Protocol modifier bits.
const MODIFIER_BITS = {
  alt: 1, ctrl: 2, control: 2, meta: 4, cmd: 4, command: 4, win: 4, windows: 4, shift: 8,
};

export function modifierBit(name) {
  return MODIFIER_BITS[String(name).toLowerCase()];
}

// "ctrl+shift" -> bitmask (unknown names are ignored)
export function parseModifierList(text) {
  let bits = 0;
  for (const part of String(text || '').toLowerCase().split('+')) {
    bits |= MODIFIER_BITS[part.trim()] || 0;
  }
  return bits;
}

const NAMED_KEYS = {
  enter: { key: 'Enter', code: 'Enter', vk: 13, text: '\r' },
  return: { key: 'Enter', code: 'Enter', vk: 13, text: '\r' },
  tab: { key: 'Tab', code: 'Tab', vk: 9 },
  escape: { key: 'Escape', code: 'Escape', vk: 27 },
  esc: { key: 'Escape', code: 'Escape', vk: 27 },
  backspace: { key: 'Backspace', code: 'Backspace', vk: 8 },
  delete: { key: 'Delete', code: 'Delete', vk: 46 },
  del: { key: 'Delete', code: 'Delete', vk: 46 },
  insert: { key: 'Insert', code: 'Insert', vk: 45 },
  space: { key: ' ', code: 'Space', vk: 32, text: ' ' },
  arrowup: { key: 'ArrowUp', code: 'ArrowUp', vk: 38 },
  up: { key: 'ArrowUp', code: 'ArrowUp', vk: 38 },
  arrowdown: { key: 'ArrowDown', code: 'ArrowDown', vk: 40 },
  down: { key: 'ArrowDown', code: 'ArrowDown', vk: 40 },
  arrowleft: { key: 'ArrowLeft', code: 'ArrowLeft', vk: 37 },
  left: { key: 'ArrowLeft', code: 'ArrowLeft', vk: 37 },
  arrowright: { key: 'ArrowRight', code: 'ArrowRight', vk: 39 },
  right: { key: 'ArrowRight', code: 'ArrowRight', vk: 39 },
  home: { key: 'Home', code: 'Home', vk: 36 },
  end: { key: 'End', code: 'End', vk: 35 },
  pageup: { key: 'PageUp', code: 'PageUp', vk: 33 },
  pagedown: { key: 'PageDown', code: 'PageDown', vk: 34 },
};
for (let i = 1; i <= 12; i++) {
  NAMED_KEYS['f' + i] = { key: 'F' + i, code: 'F' + i, vk: 111 + i };
}

const PUNCTUATION = {
  '-': { code: 'Minus', vk: 189 }, '=': { code: 'Equal', vk: 187 }, '[': { code: 'BracketLeft', vk: 219 },
  ']': { code: 'BracketRight', vk: 221 }, '\\': { code: 'Backslash', vk: 220 }, ';': { code: 'Semicolon', vk: 186 },
  "'": { code: 'Quote', vk: 222 }, ',': { code: 'Comma', vk: 188 }, '.': { code: 'Period', vk: 190 },
  '/': { code: 'Slash', vk: 191 }, '`': { code: 'Backquote', vk: 192 },
};

// macOS needs editing commands for shortcuts such as select-all to work through CDP.
const EDIT_COMMANDS = { a: 'selectAll', c: 'copy', v: 'paste', x: 'cut', z: 'undo' };

function lookupKey(name) {
  const lower = name.toLowerCase();
  if (NAMED_KEYS[lower]) return { ...NAMED_KEYS[lower] };
  if ([...name].length !== 1) return null;
  if (/[a-z]/i.test(name)) {
    return { key: name, code: 'Key' + name.toUpperCase(), vk: name.toUpperCase().charCodeAt(0), text: name };
  }
  if (/[0-9]/.test(name)) return { key: name, code: 'Digit' + name, vk: name.charCodeAt(0), text: name };
  if (PUNCTUATION[name]) return { key: name, ...PUNCTUATION[name], text: name };
  return { key: name, code: '', vk: 0, text: name };
}

// Page zoom shortcuts are rejected (use the `zoom` action instead).
export function isPageZoomShortcut(token) {
  const parts = tokenParts(token);
  const hasCmdOrCtrl = parts.mods.some((m) => ['ctrl', 'control', 'cmd', 'meta', 'command'].includes(m));
  return hasCmdOrCtrl && ['=', '+', '-', '0'].includes(parts.key);
}

function tokenParts(token) {
  const lower = token.toLowerCase();
  if (lower === '+' || !token.includes('+')) return { mods: [], key: token };
  const pieces = lower.endsWith('++') ? [...lower.slice(0, -2).split('+'), '+'] : lower.split('+');
  const key = pieces.pop();
  return { mods: pieces, key };
}

// "cmd+a" / "Enter" / "ctrl+shift+Tab" -> { key, code, vk, text?, modifiers, commands }
export function parseKeyToken(token) {
  const { mods, key } = tokenParts(token);
  let modifiers = 0;
  for (const m of mods) {
    const bit = modifierBit(m);
    if (bit === undefined) throw new ToolError('invalid_params', `Unknown modifier "${m}" in key "${token}".`);
    modifiers |= bit;
  }
  if (key === '' || key === undefined) throw new ToolError('invalid_params', `Key "${token}" is incomplete.`);
  const info = lookupKey(key);
  if (!info) throw new ToolError('invalid_params', `Unknown key "${token}". Use names such as Enter, Tab, Escape, ArrowDown, F5 or single characters.`);
  if (info.text !== undefined && (modifiers & (1 | 2 | 4))) delete info.text; // shortcuts do not type text
  if (modifiers & 8 && /^[a-z]$/.test(info.key)) info.key = info.key.toUpperCase();
  const commands = [];
  if (modifiers & (2 | 4) && !(modifiers & 1) && EDIT_COMMANDS[key.toLowerCase()]) {
    const cmd = EDIT_COMMANDS[key.toLowerCase()];
    commands.push(cmd === 'undo' && modifiers & 8 ? 'redo' : cmd);
  }
  return { ...info, modifiers, commands };
}
