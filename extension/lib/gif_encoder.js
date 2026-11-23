/**
 * gif_encoder.js - minimal animated GIF89a encoder (no dependencies).
 * Frames are RGBA pixel buffers; each frame gets its own 256-colour palette built from a
 * 5-bit-per-channel histogram (most frequent colours), which suits screenshots well.
 */

function buildPalette(rgba, maxColors = 256) {
  const counts = new Map();
  for (let i = 0; i < rgba.length; i += 4) {
    const key = ((rgba[i] >> 3) << 10) | ((rgba[i + 1] >> 3) << 5) | (rgba[i + 2] >> 3);
    counts.set(key, (counts.get(key) || 0) + 1);
  }
  const top = [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, maxColors);
  const palette = new Uint8Array(maxColors * 3);
  const keys = [];
  top.forEach(([key], i) => {
    const r = (key >> 10) & 31;
    const g = (key >> 5) & 31;
    const b = key & 31;
    palette[i * 3] = (r << 3) | (r >> 2);
    palette[i * 3 + 1] = (g << 3) | (g >> 2);
    palette[i * 3 + 2] = (b << 3) | (b >> 2);
    keys.push(key);
  });
  return { palette, size: top.length, keys };
}

function indexPixels(rgba, width, height, pal) {
  const index = new Uint8Array(width * height);
  const cache = new Map();
  pal.keys.forEach((key, i) => cache.set(key, i));
  for (let p = 0, i = 0; p < index.length; p++, i += 4) {
    const key = ((rgba[i] >> 3) << 10) | ((rgba[i + 1] >> 3) << 5) | (rgba[i + 2] >> 3);
    let hit = cache.get(key);
    if (hit === undefined) {
      // not among the most frequent colours: nearest palette entry
      let best = 0;
      let bestDist = Infinity;
      for (let c = 0; c < pal.size; c++) {
        const dr = rgba[i] - pal.palette[c * 3];
        const dg = rgba[i + 1] - pal.palette[c * 3 + 1];
        const db = rgba[i + 2] - pal.palette[c * 3 + 2];
        const d = dr * dr + dg * dg + db * db;
        if (d < bestDist) {
          bestDist = d;
          best = c;
        }
      }
      hit = best;
      cache.set(key, hit);
    }
    index[p] = hit;
  }
  return index;
}

function lzw(index, minCodeSize) {
  const clear = 1 << minCodeSize;
  const end = clear + 1;
  const out = [];
  let cur = 0;
  let bits = 0;
  const emit = (code, size) => {
    cur |= code << bits;
    bits += size;
    while (bits >= 8) {
      out.push(cur & 255);
      cur >>= 8;
      bits -= 8;
    }
  };
  let codeSize = minCodeSize + 1;
  let next = end + 1;
  let dict = new Map();
  emit(clear, codeSize);
  let prefix = index[0];
  for (let i = 1; i < index.length; i++) {
    const k = index[i];
    const key = (prefix << 8) | k;
    const found = dict.get(key);
    if (found !== undefined) {
      prefix = found;
      continue;
    }
    emit(prefix, codeSize);
    if (next < 4096) {
      dict.set(key, next++);
      if (next > 1 << codeSize && codeSize < 12) codeSize++;
    } else {
      emit(clear, codeSize);
      dict = new Map();
      codeSize = minCodeSize + 1;
      next = end + 1;
    }
    prefix = k;
  }
  emit(prefix, codeSize);
  emit(end, codeSize);
  if (bits > 0) out.push(cur & 255);
  return out;
}

/**
 * @param {{ width: number, height: number, rgba: Uint8ClampedArray|Uint8Array, delay: number }[]} frames  delay in ms
 * @returns {Uint8Array} the GIF file
 */
export function encodeGif(frames) {
  if (!frames.length) throw new Error('No frames to encode');
  const { width, height } = frames[0];
  const bytes = [];
  const word = (n) => bytes.push(n & 255, (n >> 8) & 255);
  for (const ch of 'GIF89a') bytes.push(ch.charCodeAt(0));
  word(width);
  word(height);
  bytes.push(0x00, 0, 0); // no global colour table
  // looping extension
  bytes.push(0x21, 0xff, 0x0b);
  for (const ch of 'NETSCAPE2.0') bytes.push(ch.charCodeAt(0));
  bytes.push(0x03, 0x01, 0x00, 0x00, 0x00);

  for (const frame of frames) {
    const pal = buildPalette(frame.rgba);
    let bitsNeeded = 1;
    while (1 << bitsNeeded < Math.max(2, pal.size)) bitsNeeded++;
    const tableSize = 1 << bitsNeeded;
    const index = indexPixels(frame.rgba, frame.width, frame.height, pal);
    // graphic control extension
    bytes.push(0x21, 0xf9, 0x04, 0x04);
    word(Math.max(2, Math.round(frame.delay / 10)));
    bytes.push(0x00, 0x00);
    // image descriptor with local colour table
    bytes.push(0x2c);
    word(0);
    word(0);
    word(frame.width);
    word(frame.height);
    bytes.push(0x80 | (bitsNeeded - 1));
    for (let i = 0; i < tableSize * 3; i++) bytes.push(pal.palette[i] || 0);
    const minCode = Math.max(2, bitsNeeded);
    bytes.push(minCode);
    const data = lzw(index, minCode);
    for (let i = 0; i < data.length; i += 255) {
      const chunk = data.slice(i, i + 255);
      bytes.push(chunk.length, ...chunk);
    }
    bytes.push(0x00);
  }
  bytes.push(0x3b);
  return Uint8Array.from(bytes);
}
