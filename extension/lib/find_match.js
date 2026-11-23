/**
 * find_match.js - local (no LLM) matcher behind the `find` tool.
 *
 * Input: the nodes produced by page_tree.js with { findData: true } and a natural-language
 * query. Query words are normalised (accents, camelCase, light stemming), role words such as
 * "button" or "field" select node roles, and the remaining words are scored against each node's
 * name / label / placeholder (weight 1), ids and href (0.5) and surrounding container text (0.3).
 */

const DEFAULT_ROLES = new Set(
  'button file-input link textbox searchbox combobox listbox checkbox radio switch tab menuitem option slider spinbutton'.split(' ')
);

const STOP_WORDS = new Set(
  ('the a an of for to in on at with and or this that these is are be it its please me my your our i you we find locate get show click press tap hit select choose open go enter type fill use called named labeled labelled titled says which where element thing page one can want need from by near about into under above below inside within next to there here').split(' ')
);

const ROLE_WORDS = new Map();
for (const [words, roles] of [
  ['button btn', 'button file-input'],
  ['link hyperlink', 'link'],
  ['field input box textbox textfield textarea', 'textbox searchbox combobox'],
  ['checkbox tickbox', 'checkbox'],
  ['radio', 'radio'],
  ['dropdown select selector picker combobox', 'combobox listbox'],
  ['tab', 'tab'],
  ['heading headline title', 'heading'],
  ['image img picture photo icon logo thumbnail', 'image'],
  ['toggle switch', 'switch checkbox'],
  ['slider', 'slider'],
  ['option', 'option'],
  ['menuitem', 'menuitem'],
]) {
  for (const word of words.split(' ')) ROLE_WORDS.set(word, roles.split(' '));
}

const WORD = /[\p{L}\p{N}]+/gu;
const CJK = /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Thai}]/u;
const HAN = /\p{Script=Han}/u;

const stripAccents = (s) => s.normalize('NFKD').replace(/\p{M}+/gu, '');
const splitCamel = (s) => s.replace(/([a-z0-9])([A-Z])/g, '$1 $2');

export function tokenize(text) {
  if (!text) return [];
  return splitCamel(stripAccents(text)).toLowerCase().match(WORD) || [];
}

export function stem(word) {
  if (word.length <= 3) return word;
  return word.replace(/ies$/, 'y').replace(/(ing|ed|es|s)$/, '') || word;
}

function tokenSet(parts) {
  const set = new Set();
  for (const part of parts) {
    if (!part) continue;
    const plain = stripAccents(part);
    const spaced = splitCamel(plain);
    for (const w of spaced.toLowerCase().match(WORD) || []) set.add(stem(w));
    if (spaced !== plain) for (const w of plain.toLowerCase().match(WORD) || []) set.add(stem(w));
  }
  return set;
}

// One typo / one-edit tolerance, prefixes, and substring match for CJK text.
function fuzzyMatch(a, b) {
  if ((a.length >= 4 && b.startsWith(a)) || (b.length >= 4 && a.startsWith(b))) return true;
  if (a.length >= 5 && withinOneEdit(a, b)) return true;
  return CJK.test(a) && (a.length >= 3 || (a.length === 2 && HAN.test(a))) && b.includes(a);
}

function withinOneEdit(a, b) {
  if (Math.abs(a.length - b.length) > 1) return false;
  let i = 0;
  let j = 0;
  let edits = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      i++;
      j++;
      continue;
    }
    if (++edits > 1) return false;
    if (a.length > b.length) i++;
    else if (b.length > a.length) j++;
    else {
      i++;
      j++;
    }
  }
  return edits + (a.length - i) + (b.length - j) <= 1;
}

function bestText(node) {
  const t = (node.meta && node.meta.texts) || {};
  return node.name || t.aria || t.text || t.placeholder || node.placeholder || t.title || t.alt || t.value || '';
}

function quoted(text, max) {
  let s = String(text).replace(/\s+/g, ' ').trim();
  if (s.length > max) s = s.slice(0, max - 1) + '…';
  return '"' + s.replace(/[\\"]/g, '\\$&') + '"';
}

function score(nodes, query) {
  const words = tokenize(query);
  const roleSet = new Set();
  const content = [];
  for (const w of words) {
    const roles = ROLE_WORDS.get(w) || ROLE_WORDS.get(stem(w));
    if (roles) roles.forEach((r) => roleSet.add(r));
    else if (!STOP_WORDS.has(w)) content.push(w);
  }
  const roleOnly = content.length === 0 && roleSet.size > 0;
  if (content.length === 0) content.push(...words.slice(0, 16));
  const stems = [...new Set(content.map(stem))].slice(0, 16);
  const phrase = content.join(' ');

  const scored = [];
  nodes.forEach((node, index) => {
    const roleMatch = roleSet.has(node.role);
    if (roleOnly && !roleMatch) return;
    const t = (node.meta && node.meta.texts) || {};
    const groups = [
      [tokenSet([node.name, t.label, t.aria, t.placeholder || node.placeholder, t.title, t.alt, t.value, t.text,
        node.role === 'file-input' ? 'file files upload attach' : undefined, ...(node.options || []).slice(0, 30)]), 1],
      [tokenSet([t.ids, node.href]), 0.5],
      [tokenSet([t.container, t.describedby]), 0.3],
    ];
    let total = 0;
    for (const word of stems) {
      let best = 0;
      for (const [set, weight] of groups) {
        if (set.has(word)) best = Math.max(best, weight);
        else if (best < weight) {
          for (const candidate of set) {
            if (fuzzyMatch(word, candidate)) {
              best = Math.max(best, 0.7 * weight);
              break;
            }
          }
        }
      }
      total += best;
    }
    let s = stems.length ? total / stems.length : 0;
    if (!s && !roleOnly) return;
    if (roleMatch) s += 0.3;
    const plainName = stripAccents(node.name || '');
    const hasPhrase = (x) => x.toLowerCase().includes(phrase);
    if (phrase && (hasPhrase(splitCamel(plainName)) || hasPhrase(plainName))) s += 0.3;
    if (node.meta && !node.meta.visible) s *= 0.8;
    scored.push({ node, index, score: s });
  });
  scored.sort((a, b) => b.score - a.score || a.index - b.index);

  // Then pad with controls of the requested kind, and visible on-screen controls.
  const seen = new Set(scored.map((s) => s.index));
  const pad = (pred) =>
    nodes.forEach((node, index) => {
      if (!seen.has(index) && pred(node)) {
        seen.add(index);
        scored.push({ node, index, score: 0 });
      }
    });
  if (roleSet.size) pad((n) => roleSet.has(n.role));
  pad((n) => DEFAULT_ROLES.has(n.role) && !!(n.meta && n.meta.visible && n.meta.inViewport));
  return scored;
}

function describe(shown, matching) {
  const nodes = shown.map((s) => s.node);
  const key = (n) => n.role + '\0' + bestText(n).toLowerCase();
  const counts = new Map();
  for (const n of nodes) counts.set(key(n), (counts.get(key(n)) || 0) + 1);
  const containers = new Map();
  for (const n of nodes) {
    const c = n.meta && n.meta.texts.container;
    if (c) containers.set(key(n), [...(containers.get(key(n)) || []), c]);
  }
  const commonPrefix = new Map();
  for (const [k, list] of containers) {
    let prefix = list.length > 1 ? list[0] : '';
    for (const c of list) {
      let i = 0;
      while (i < prefix.length && i < c.length && prefix[i] === c[i]) i++;
      prefix = prefix.slice(0, i);
    }
    commonPrefix.set(k, prefix);
  }

  const lines = nodes.map((n) => {
    const text = bestText(n);
    const label = n.meta && n.meta.texts.label;
    let line = n.ref + ' ' + String(n.role).slice(0, 40).replace(/[^A-Za-z-]+/g, '');
    if (text) line += ' ' + quoted(text, 60);
    if (label && !text.toLowerCase().includes(label.toLowerCase())) line += ' label ' + quoted(label, 40);
    const container = n.meta && n.meta.texts.container;
    const prefix = commonPrefix.get(key(n)) || '';
    if ((counts.get(key(n)) || 0) > 1 && container && prefix !== container) {
      const start = prefix.replace(/\S*$/, '').length;
      let rest = container.slice(start);
      if (text && rest.toLowerCase().startsWith(text.toLowerCase())) rest = rest.slice(text.length);
      if (rest.trim().length >= 4) line += ' in ' + quoted(rest, 50);
    }
    const flags = [];
    if (n.meta && !n.meta.visible) flags.push('hidden');
    else if (n.meta && !n.meta.inViewport) flags.push('off-screen');
    if (n.meta && n.meta.disabled) flags.push('disabled');
    return flags.length ? `${line} (${flags.join(', ')})` : line;
  });

  const shownMatching = Math.min(matching, nodes.length);
  const head =
    matching +
    (matching === 1 ? ' element shares' : ' elements share') +
    ' words with the query' +
    (matching > nodes.length ? '; the ' + nodes.length + ' closest' : '') +
    (nodes.length > shownMatching ? ', then other controls of the kind asked for or on screen' : '') +
    ':\n';
  return head + lines.join('\n');
}

/**
 * @param {object} data  { findNodes, findTruncated, findMetaPartial } from page_tree.js
 * @returns {{ output?: string, noMatch?: string, refs: string[], matches: number, total: number }}
 */
export function findLocal(data, query) {
  if (!Array.isArray(data.findNodes)) {
    throw new Error("Could not read this page's elements. Reload the tab (unsaved input on the page is lost), then call find again.");
  }
  const ranked = score(data.findNodes, query);
  const top = ranked.slice(0, 20);
  const matching = ranked.filter((r) => r.score > 0).length;
  const notes = [
    ranked[20] && ranked[20].score && ranked[20].score === ranked[0].score ? 'To see the others, use a more specific query.' : '',
    data.findTruncated === true ? 'Only the first part of the page was searched, so an element further down may be missing.' : '',
    data.findMetaPartial === true
      ? 'Elements late in the page were searched by their names only, and their lines do not say whether they are hidden, off-screen or disabled.'
      : '',
  ].filter(Boolean);
  const suffix = notes.length ? '\n\n' + notes.join(' ') : '';
  if (top.length === 0) {
    return {
      noMatch: 'No matching elements found. Try words that appear on the page, or use read_page to list its elements.' + suffix,
      refs: [],
      matches: 0,
      total: 0,
    };
  }
  return {
    output: describe(top, matching) + suffix,
    refs: top.map((r) => r.node.ref),
    matches: Math.min(matching, 20),
    total: matching,
  };
}
