/**
 * page_text.js - get_page_text: plain text of the main content, injected with executeScript({ func }).
 * Must stay self-contained.
 */
export function extractPageText(maxChars) {
  const selectors = [
    'article', 'main', '[class*="articleBody"]', '[class*="article-body"]', '[class*="post-content"]',
    '[class*="entry-content"]', '[class*="content-body"]', '[role="main"]', '.content', '#content',
  ];
  let source = null;
  for (const selector of selectors) {
    const found = document.querySelectorAll(selector);
    if (found.length > 0) {
      // several matches: take the one with the most text
      let best = found[0];
      let bestLen = 0;
      found.forEach((el) => {
        const len = (el.innerText || '').length;
        if (len > bestLen) {
          bestLen = len;
          best = el;
        }
      });
      source = best;
      break;
    }
  }
  if (!source) source = document.body;

  const clean = (el) =>
    (el.innerText || '')
      .replace(/[^\S\n]+/g, ' ')
      .replace(/ ?\n ?/g, '\n')
      .replace(/\n{3,}/g, '\n\n')
      .trim();

  let text = clean(source);
  let fellBack = false;
  if (text.length < 10 && document.body && source !== document.body) {
    fellBack = true;
    source = document.body;
    text = clean(source);
  }
  const info = { title: document.title, url: window.location.href };
  if (text.length < 10) {
    return { ...info, text: '', source: 'none', error: 'No text content found. Page may contain only images, videos, or canvas-based content.' };
  }
  if (text.length > maxChars) {
    const total = text.length;
    let cut = text.lastIndexOf('\n', maxChars);
    if (cut <= 0) cut = Math.max(0, maxChars);
    text =
      text.slice(0, cut) +
      '\n\n[output truncated at ' + maxChars + ' of ' + total +
      ' characters. Pass a larger max_chars (default 50000) to see more, or use read_page with a ref_id to focus on a smaller section.]';
  }
  return { ...info, text, source: source.tagName.toLowerCase(), fellBack };
}
