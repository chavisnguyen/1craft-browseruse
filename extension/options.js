document.addEventListener('DOMContentLoaded', async () => {
  const form = document.getElementById('optionsForm');
  const labelInput = document.getElementById('labelInput');
  const tokenInput = document.getElementById('tokenInput');
  const daemonUrlInput = document.getElementById('daemonUrlInput');
  const windowModeSelect = document.getElementById('windowModeSelect');
  const connStatus = document.getElementById('connStatus');
  const userEmail = document.getElementById('userEmail');
  const instanceIdSpan = document.getElementById('instanceId');
  const alertBox = document.getElementById('alertBox');

  function showAlert(msg, isError = false) {
    alertBox.textContent = msg;
    alertBox.className = isError ? 'alert alert-error' : 'alert alert-success';
    alertBox.style.display = 'block';
    setTimeout(() => {
      alertBox.style.display = 'none';
    }, 4000);
  }

  // Load saved options
  const data = await chrome.storage.local.get([
    'label',
    'token',
    'daemonUrl',
    'windowMode',
    'instanceId',
  ]);

  if (data.label) labelInput.value = data.label;
  if (data.token) tokenInput.value = data.token;
  if (data.daemonUrl) daemonUrlInput.value = data.daemonUrl;
  if (data.windowMode) windowModeSelect.value = data.windowMode;
  if (data.instanceId) instanceIdSpan.textContent = data.instanceId;

  // If no saved token, attempt prefill from default_config.json
  if (!data.token) {
    try {
      const resp = await fetch(chrome.runtime.getURL('default_config.json'));
      if (resp.ok) {
        const def = await resp.json();
        if (def.label && !labelInput.value) labelInput.value = def.label;
        if (def.token && !tokenInput.value) tokenInput.value = def.token;
        if (def.daemonUrl && !daemonUrlInput.value) daemonUrlInput.value = def.daemonUrl;
        if (def.windowMode && !windowModeSelect.value) windowModeSelect.value = def.windowMode;
      }
    } catch {
      // ignore
    }
  }

  // Retrieve user email via chrome.identity (if signed in)
  if (chrome.identity && chrome.identity.getProfileUserInfo) {
    try {
      chrome.identity.getProfileUserInfo({ accountStatus: 'ANY' }, (info) => {
        if (chrome.runtime.lastError) {
          userEmail.textContent = 'Không lấy được (' + chrome.runtime.lastError.message + ')';
        } else if (info && info.email) {
          userEmail.textContent = info.email;
        } else {
          userEmail.textContent = 'Chưa đăng nhập Chrome';
        }
      });
    } catch (e) {
      userEmail.textContent = 'Lỗi: ' + e.message;
    }
  } else {
    userEmail.textContent = 'API identity không khả dụng';
  }

  // Check connection status from background
  function refreshStatus() {
    chrome.runtime.sendMessage({ type: 'getStatus' }, (resp) => {
      if (chrome.runtime.lastError) {
        connStatus.textContent = 'Offline';
        connStatus.className = 'badge badge-off';
        return;
      }
      if (resp && resp.connected) {
        connStatus.textContent = 'Đã kết nối (Online)';
        connStatus.className = 'badge badge-on';
      } else {
        connStatus.textContent = 'Chưa kết nối (Offline)';
        connStatus.className = 'badge badge-off';
      }
    });
  }

  refreshStatus();
  setInterval(refreshStatus, 2000);

  // Form submit
  form.addEventListener('submit', async (e) => {
    e.preventDefault();

    const label = labelInput.value.trim();
    const token = tokenInput.value.trim();
    const daemonUrl = daemonUrlInput.value.trim();
    const windowMode = windowModeSelect.value;

    const labelRegex = /^[a-zA-Z0-9_-]{1,32}$/;
    if (!labelRegex.test(label)) {
      showAlert('Tên hồ sơ không hợp lệ (1-32 ký tự alphanumeric, _, -)', true);
      return;
    }

    if (token.length !== 64) {
      showAlert('Mã xác thực token phải có đúng 64 ký tự hex', true);
      return;
    }

    await chrome.storage.local.set({
      label,
      token,
      daemonUrl,
      windowMode,
    });

    // Notify background to reconnect
    chrome.runtime.sendMessage({ type: 'reconnect' }, () => {
      showAlert('Đã lưu cấu hình và gửi yêu cầu kết nối lại!');
      setTimeout(refreshStatus, 500);
    });
  });
});
