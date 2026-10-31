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

  const statusDot = document.getElementById('statusDot');
  const statusText = document.getElementById('statusText');
  const labelInput = document.getElementById('labelInput');
  const saveBtn = document.getElementById('saveBtn');
  const emailVal = document.getElementById('emailVal');
  const tabsVal = document.getElementById('tabsVal');
  const optionsBtn = document.getElementById('optionsBtn');
  const reconnectBtn = document.getElementById('reconnectBtn');
  const feedback = document.getElementById('feedback');

  function showFeedback(msg, isError = false) {
    feedback.textContent = msg;
    feedback.className = isError ? 'feedback error' : 'feedback';
    feedback.style.display = 'block';
    setTimeout(() => {
      feedback.style.display = 'none';
    }, 3000);
  }

  // 1. Load initial label and email
  const data = await chrome.storage.local.get(['label', 'token', 'email']);
  if (data.label) {
    labelInput.value = data.label;
  }

  // 2. Resolve account email
  if (data.email) {
    emailVal.textContent = data.email;
  } else if (chrome.identity && chrome.identity.getProfileUserInfo) {
    chrome.identity.getProfileUserInfo({ accountStatus: 'ANY' }, (info) => {
      if (info && info.email) {
        emailVal.textContent = info.email;
        chrome.storage.local.set({ email: info.email });
      } else {
        emailVal.textContent = chrome.i18n.getMessage('localProfile') || 'Local profile';
      }
    });
  } else {
    emailVal.textContent = chrome.i18n.getMessage('localProfile') || 'Local profile';
  }

  // 3. Status poller
  function refreshStatus() {
    chrome.runtime.sendMessage({ type: 'getStatus' }, (resp) => {
      if (chrome.runtime.lastError || !resp) {
        statusDot.className = 'dot disconnected';
        statusText.textContent = chrome.i18n.getMessage('statusDisconnected') || 'Disconnected';
        tabsVal.textContent = '0';
        return;
      }
      if (resp.connected) {
        statusDot.className = 'dot connected';
        statusText.textContent = chrome.i18n.getMessage('statusConnected') || 'Connected';
      } else {
        statusDot.className = 'dot disconnected';
        statusText.textContent = chrome.i18n.getMessage('statusDisconnected') || 'Disconnected';
      }
      if (resp.activeTabIds) {
        tabsVal.textContent = String(resp.activeTabIds.length);
      }
    });
  }

  refreshStatus();
  setInterval(refreshStatus, 1500);

  // 4. Save label
  saveBtn.addEventListener('click', async () => {
    const newLabel = labelInput.value.trim();
    const labelRegex = /^[a-zA-Z0-9_.-]{1,32}$/;
    if (!labelRegex.test(newLabel)) {
      showFeedback(chrome.i18n.getMessage('invalidLabel'), true);
      return;
    }

    saveBtn.disabled = true;
    saveBtn.textContent = chrome.i18n.getMessage('saving') || 'Saving...';

    await chrome.storage.local.set({ label: newLabel });
    chrome.runtime.sendMessage({ type: 'reconnect' }, () => {
      saveBtn.disabled = false;
      saveBtn.textContent = chrome.i18n.getMessage('save') || 'Save';
      showFeedback(chrome.i18n.getMessage('saved') || 'Saved');
      setTimeout(refreshStatus, 400);
    });
  });

  labelInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      saveBtn.click();
    }
  });

  // 5. Open settings
  optionsBtn.addEventListener('click', () => {
    if (chrome.runtime.openOptionsPage) {
      chrome.runtime.openOptionsPage();
    } else {
      window.open(chrome.runtime.getURL('options.html'));
    }
  });

  // 6. Manual reconnect
  reconnectBtn.addEventListener('click', () => {
    chrome.runtime.sendMessage({ type: 'reconnect' }, () => {
      showFeedback(chrome.i18n.getMessage('reconnecting') || 'Reconnecting...');
      setTimeout(refreshStatus, 500);
    });
  });
});
