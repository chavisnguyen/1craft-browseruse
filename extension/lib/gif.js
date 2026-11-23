/**
 * gif.js - gif_creator: records one frame per computer/navigate action and exports an animated
 * GIF with overlays (click circles, drag arrows, action labels, progress bar, watermark).
 */
import { encodeGif } from './gif_encoder.js';

const MAX_FRAMES = 50;
const MAX_WIDTH = 900;

const state = { recording: false, frames: [] };

export const isRecording = () => state.recording;
export const frameCount = () => state.frames.length;

export function startRecording() {
  if (state.recording) {
    return { output: "Recording is already active for this tab group. Use 'stop_recording' to stop or 'export' to generate GIF." };
  }
  state.frames = [];
  state.recording = true;
  return {
    output: 'Started recording browser actions for this tab group. All computer and navigate tool actions will now be captured (max 50 frames). Previous frames cleared.',
  };
}

export function stopRecording() {
  if (!state.recording) return { output: "Recording is not active for this tab group. Use 'start_recording' to begin capturing." };
  state.recording = false;
  const n = state.frames.length;
  return { output: `Stopped recording for this tab group. Captured ${n} frame${n === 1 ? '' : 's'}. Use 'export' to generate GIF or 'clear' to discard.` };
}

export function clearFrames() {
  const n = state.frames.length;
  state.frames = [];
  state.recording = false;
  return n;
}

export function clearAction() {
  const n = clearFrames();
  return { output: n === 0 ? 'No frames to clear for this tab group.' : `Cleared ${n} frame${n === 1 ? '' : 's'} for this tab group. Recording stopped.` };
}

export function addFrame(frame) {
  state.frames.push(frame);
  if (state.frames.length > MAX_FRAMES) state.frames.shift();
}

const DELAYS = {
  wait: 300, screenshot: 300, navigate: 800, scroll: 800, scroll_to: 800, type: 800, key: 800, zoom: 800,
  left_click: 1500, right_click: 1500, double_click: 1500, triple_click: 1500, left_click_drag: 1500,
};

function delayFor(action) {
  if (!action) return 800;
  return DELAYS[action.type] ?? 800;
}

function labelFor(action) {
  if (!action) return '';
  const at = (c) => (c ? ` (${Math.round(c[0])}, ${Math.round(c[1])})` : '');
  switch (action.type) {
    case 'left_click':
      return `Click${at(action.coordinate)}${action.ref ? ' ' + action.ref : ''}`;
    case 'right_click':
      return `Right click${at(action.coordinate)}`;
    case 'double_click':
      return `Double click${at(action.coordinate)}`;
    case 'triple_click':
      return `Triple click${at(action.coordinate)}`;
    case 'left_click_drag':
      return `Drag${at(action.start_coordinate)} → ${at(action.coordinate).trim()}`;
    case 'type':
      return `Type: "${String(action.text || '').slice(0, 40)}"`;
    case 'key':
      return `Key: ${String(action.text || '').slice(0, 40)}`;
    case 'scroll':
      return `Scroll ${action.scroll_direction || 'down'}`;
    case 'navigate':
      return `Navigate: ${String(action.url || '').slice(0, 50)}`;
    case 'hover':
      return `Hover${at(action.coordinate)}`;
    case 'zoom':
      return 'Zoom';
    case 'wait':
      return 'Wait';
    default:
      return action.type;
  }
}

async function render(frame, options, index, total) {
  const blob = await (await fetch(`data:image/jpeg;base64,${frame.base64}`)).blob();
  const bmp = await createImageBitmap(blob);
  const scale = Math.min(1, MAX_WIDTH / bmp.width);
  const width = Math.max(1, Math.round(bmp.width * scale));
  const height = Math.max(1, Math.round(bmp.height * scale));
  const canvas = new OffscreenCanvas(width, height);
  const ctx = canvas.getContext('2d', { willReadFrequently: true });
  ctx.drawImage(bmp, 0, 0, width, height);
  bmp.close();

  // action coordinates are in the screenshot frame; map them onto the rendered frame
  const sx = width / (frame.frameWidth || width);
  const sy = height / (frame.frameHeight || height);
  const act = frame.action;
  if (act && options.showClickIndicators && act.coordinate && /click/.test(act.type)) {
    const x = act.coordinate[0] * sx;
    const y = act.coordinate[1] * sy;
    ctx.beginPath();
    ctx.arc(x, y, 18, 0, Math.PI * 2);
    ctx.fillStyle = 'rgba(255, 140, 0, 0.25)';
    ctx.fill();
    ctx.lineWidth = 3;
    ctx.strokeStyle = '#FF8C00';
    ctx.stroke();
  }
  if (act && options.showDragPaths && act.type === 'left_click_drag' && act.start_coordinate && act.coordinate) {
    const x1 = act.start_coordinate[0] * sx;
    const y1 = act.start_coordinate[1] * sy;
    const x2 = act.coordinate[0] * sx;
    const y2 = act.coordinate[1] * sy;
    ctx.strokeStyle = '#E53935';
    ctx.fillStyle = '#E53935';
    ctx.lineWidth = 3;
    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x2, y2);
    ctx.stroke();
    const angle = Math.atan2(y2 - y1, x2 - x1);
    ctx.beginPath();
    ctx.moveTo(x2, y2);
    ctx.lineTo(x2 - 14 * Math.cos(angle - 0.4), y2 - 14 * Math.sin(angle - 0.4));
    ctx.lineTo(x2 - 14 * Math.cos(angle + 0.4), y2 - 14 * Math.sin(angle + 0.4));
    ctx.closePath();
    ctx.fill();
  }
  if (act && options.showActionLabels) {
    const text = labelFor(act);
    if (text) {
      ctx.font = '14px sans-serif';
      const w = Math.min(width - 16, ctx.measureText(text).width + 16);
      ctx.fillStyle = 'rgba(0, 0, 0, 0.8)';
      ctx.fillRect(8, height - 40, w, 26);
      ctx.fillStyle = '#fff';
      ctx.fillText(text, 16, height - 22, w - 16);
    }
  }
  if (options.showProgressBar) {
    ctx.fillStyle = '#FF8C00';
    ctx.fillRect(0, height - 4, Math.round((width * (index + 1)) / total), 4);
  }
  if (options.showWatermark) {
    ctx.font = 'bold 12px sans-serif';
    ctx.fillStyle = 'rgba(255, 255, 255, 0.85)';
    ctx.strokeStyle = 'rgba(0, 0, 0, 0.5)';
    ctx.lineWidth = 3;
    const mark = 'klik';
    const x = width - ctx.measureText(mark).width - 10;
    ctx.strokeText(mark, x, 18);
    ctx.fillText(mark, x, 18);
  }
  const image = ctx.getImageData(0, 0, width, height);
  return { width, height, rgba: image.data, delay: delayFor(act) };
}

/** Encodes the recorded frames. Returns { base64, size, width, height, frames }. */
export async function buildGif(rawOptions = {}) {
  const frames = state.frames;
  const options = {
    showClickIndicators: rawOptions.showClickIndicators ?? true,
    showDragPaths: rawOptions.showDragPaths ?? true,
    showActionLabels: rawOptions.showActionLabels ?? true,
    showProgressBar: rawOptions.showProgressBar ?? true,
    showWatermark: rawOptions.showWatermark ?? true,
  };
  const rendered = [];
  for (let i = 0; i < frames.length; i++) rendered.push(await render(frames[i], options, i, frames.length));
  const bytes = encodeGif(rendered);
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return { base64: btoa(binary), size: bytes.length, width: rendered[0].width, height: rendered[0].height, frames: frames.length };
}

export function framesForExport() {
  return state.frames;
}
