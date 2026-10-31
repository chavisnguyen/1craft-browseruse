function localizeDOM() {
  document.querySelectorAll('[data-i18n]').forEach((el) => {
    const key = el.getAttribute('data-i18n');
    const msg = chrome.i18n.getMessage(key);
    if (msg) el.textContent = msg;
  });
  document.querySelectorAll('[data-i18n-placeholder]').forEach((el) => {
    const key = el.getAttribute('data-i18n-placeholder');
    const msg = chrome.i18n.getMessage(key);
    if (msg) el.setAttribute('placeholder', msg);
  });
}

document.addEventListener('DOMContentLoaded', async () => {
  localizeDOM();

  const form = document.getElementById('optionsForm');
  const labelInput = document.getElementById('labelInput');
  const tokenInput = document.getElementById('tokenInput');
  const daemonUrlInput = document.getElementById('daemonUrlInput');
  const windowModeSelect = document.getElementById('windowModeSelect');
  const connDot = document.getElementById('connDot');
  const connStatusText = document.getElementById('connStatusText');
  const userEmail = document.getElementById('userEmail');
  const instanceIdSpan = document.getElementById('instanceId');
  const alertBox = document.getElementById('alertBox');
  const saveBtn = document.getElementById('saveBtn');

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
    'email',
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
  if (data.email) {
    userEmail.textContent = data.email;
  } else if (chrome.identity && chrome.identity.getProfileUserInfo) {
    try {
      chrome.identity.getProfileUserInfo({ accountStatus: 'ANY' }, (info) => {
        if (info && info.email) {
          userEmail.textContent = info.email;
          chrome.storage.local.set({ email: info.email });
        } else {
          userEmail.textContent = chrome.i18n.getMessage('localProfile') || 'Local profile';
        }
      });
    } catch (e) {
      userEmail.textContent = chrome.i18n.getMessage('localProfile') || 'Local profile';
    }
  } else {
    userEmail.textContent = chrome.i18n.getMessage('localProfile') || 'Local profile';
  }

  // Check connection status from background
  function refreshStatus() {
    chrome.runtime.sendMessage({ type: 'getStatus' }, (resp) => {
      if (chrome.runtime.lastError || !resp) {
        connDot.className = 'dot disconnected';
        connStatusText.textContent = chrome.i18n.getMessage('statusDisconnected') || 'Disconnected';
        return;
      }
      if (resp.connected) {
        connDot.className = 'dot connected';
        connStatusText.textContent = chrome.i18n.getMessage('statusConnected') || 'Connected';
      } else {
        connDot.className = 'dot disconnected';
        connStatusText.textContent = chrome.i18n.getMessage('statusDisconnected') || 'Disconnected';
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

    const labelRegex = /^[a-zA-Z0-9_.-]{1,32}$/;
    if (!labelRegex.test(label)) {
      showAlert(chrome.i18n.getMessage('invalidLabel') || 'Invalid name', true);
      return;
    }

    if (token.length !== 64) {
      showAlert(chrome.i18n.getMessage('daemonTokenHelp') || 'Token must be 64 hex characters', true);
      return;
    }

    saveBtn.disabled = true;
    saveBtn.textContent = chrome.i18n.getMessage('saving') || 'Saving...';

    await chrome.storage.local.set({
      label,
      token,
      daemonUrl,
      windowMode,
    });

    // Notify background to reconnect
    chrome.runtime.sendMessage({ type: 'reconnect' }, () => {
      saveBtn.disabled = false;
      saveBtn.textContent = chrome.i18n.getMessage('save') || 'Save';
      showAlert(chrome.i18n.getMessage('savedSettings') || 'Settings saved');
      setTimeout(refreshStatus, 500);
    });
  });
});
