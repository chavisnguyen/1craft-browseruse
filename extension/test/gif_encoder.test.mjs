import test from 'node:test';
import assert from 'node:assert/strict';
import { encodeGif } from '../lib/gif_encoder.js';

function frame(w, h, rgb, delay = 500) {
  const rgba = new Uint8Array(w * h * 4);
  for (let i = 0; i < w * h; i++) {
    rgba[i * 4] = rgb[0] + ((i % w) % 7);
    rgba[i * 4 + 1] = rgb[1];
    rgba[i * 4 + 2] = rgb[2];
    rgba[i * 4 + 3] = 255;
  }
  return { width: w, height: h, rgba, delay };
}

test('encodes a valid animated GIF header, loop block and trailer', () => {
  const gif = encodeGif([frame(40, 30, [200, 10, 10]), frame(40, 30, [10, 200, 10], 1000)]);
  assert.equal(String.fromCharCode(...gif.slice(0, 6)), 'GIF89a');
  assert.equal(gif[6] | (gif[7] << 8), 40);
  assert.equal(gif[8] | (gif[9] << 8), 30);
  assert.equal(gif[gif.length - 1], 0x3b);
  assert.ok(String.fromCharCode(...gif).includes('NETSCAPE2.0'));
  // two image descriptors
  assert.equal([...gif].filter((b, i) => b === 0x2c && gif[i + 1] === 0 && gif[i + 2] === 0).length >= 2, true);
});

test('handles large noisy frames (palette overflow, dictionary reset)', () => {
  const w = 120;
  const h = 90;
  const rgba = new Uint8Array(w * h * 4);
  let seed = 7;
  for (let i = 0; i < rgba.length; i++) {
    seed = (seed * 1103515245 + 12345) & 0x7fffffff;
    rgba[i] = i % 4 === 3 ? 255 : seed >> 16;
  }
  const gif = encodeGif([{ width: w, height: h, rgba, delay: 100 }]);
  assert.ok(gif.length > 1000);
  assert.equal(gif[gif.length - 1], 0x3b);
});

test('rejects empty input', () => {
  assert.throws(() => encodeGif([]), /No frames/);
});
