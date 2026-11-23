import test from 'node:test';
import assert from 'node:assert/strict';
import { findLocal, tokenize, stem } from '../lib/find_match.js';

const meta = (texts = {}, extra = {}) => ({ visible: true, inViewport: true, disabled: false, texts, ...extra });
const nodes = [
  { ref: 'ref_1', role: 'textbox', name: 'Search products', placeholder: 'Search products', meta: meta({ placeholder: 'Search products' }) },
  { ref: 'ref_2', role: 'button', name: 'Add to cart', meta: meta({ text: 'Add to cart', container: 'Organic mango 3.99 Add to cart' }) },
  { ref: 'ref_3', role: 'button', name: 'Add to cart', meta: meta({ text: 'Add to cart', container: 'Green apple 1.20 Add to cart' }) },
  { ref: 'ref_4', role: 'link', name: 'Log in', href: '/login', meta: meta({ text: 'Log in', ids: 'nav-login' }) },
  { ref: 'ref_5', role: 'button', name: 'Hidden thing', meta: meta({ text: 'Hidden thing' }, { visible: false, inViewport: false }) },
  { ref: 'ref_6', role: 'textbox', name: '', meta: meta({ label: 'Email address' }, { disabled: true }) },
];

test('tokenizer and stemmer', () => {
  assert.deepEqual(tokenize('addToCart Button'), ['add', 'to', 'cart', 'button']);
  assert.equal(stem('products'), 'product');
  assert.equal(stem('cat'), 'cat');
});

test('finds by purpose and text', () => {
  let r = findLocal({ findNodes: nodes }, 'search bar');
  assert.equal(r.refs[0], 'ref_1');
  assert.match(r.output, /^1 element shares words with the query/);
  r = findLocal({ findNodes: nodes }, 'login link');
  assert.equal(r.refs[0], 'ref_4');
});

test('role words restrict to matching roles', () => {
  const r = findLocal({ findNodes: nodes }, 'button');
  // buttons come first; other visible controls only pad the list afterwards
  assert.deepEqual(new Set(r.refs.slice(0, 3)), new Set(['ref_2', 'ref_3', 'ref_5']));
  assert.match(r.output, /^3 elements share words with the query.*then other controls/);
});

test('repeated controls are told apart by their container', () => {
  const r = findLocal({ findNodes: nodes }, 'add to cart button');
  const lines = r.output.split('\n').filter((l) => l.startsWith('ref_'));
  assert.ok(lines.some((l) => l.includes('ref_2') && l.includes('in "') && l.includes('mango')));
  assert.ok(lines.some((l) => l.includes('ref_3') && l.includes('apple')));
});

test('uses labels, flags disabled / hidden / off-screen', () => {
  let r = findLocal({ findNodes: nodes }, 'email address field');
  assert.match(r.output, /ref_6 textbox label "Email address" \(disabled\)/);
  r = findLocal({ findNodes: nodes }, 'hidden thing');
  assert.match(r.output, /ref_5 button "Hidden thing" \(hidden\)/);
});

test('fuzzy match tolerates a typo', () => {
  const r = findLocal({ findNodes: nodes }, 'serch products');
  assert.equal(r.refs[0], 'ref_1');
});

test('no nodes -> noMatch; notes about truncation', () => {
  const none = findLocal({ findNodes: [] }, 'anything');
  assert.match(none.noMatch, /No matching elements found/);
  const partial = findLocal({ findNodes: nodes, findTruncated: true, findMetaPartial: true }, 'search');
  assert.match(partial.output, /Only the first part of the page was searched/);
  assert.match(partial.output, /searched by their names only/);
  assert.throws(() => findLocal({}, 'x'), /Could not read this page's elements/);
});

test('shows at most 20 elements', () => {
  const many = Array.from({ length: 30 }, (_, i) => ({ ref: 'ref_' + i, role: 'button', name: 'Save item', meta: meta({ text: 'Save item' }) }));
  const r = findLocal({ findNodes: many }, 'save item');
  assert.equal(r.refs.length, 20);
  assert.equal(r.total, 30);
  assert.match(r.output, /^30 elements share words with the query; the 20 closest/);
  assert.match(r.output, /To see the others, use a more specific query\./);
});
