document.addEventListener('DOMContentLoaded', async () => {
  const connBadge = document.getElementById('connBadge');
  const labelInput = document.getElementById('labelInput');
  const saveBtn = document.getElementById('saveBtn');
  const emailVal = document.getElementById('emailVal');
  const tabsVal = document.getElementById('tabsVal');
  const optionsBtn = document.getElementById('optionsBtn');
  const reconnectBtn = document.getElementById('reconnectBtn');
  const toast = document.getElementById('toast');

  function showToast(msg, isError = false) {
    toast.textContent = msg;
    toast.style.background = isError ? '#ef4444' : '#10b981';
    toast.style.display = 'block';
    setTimeout(() => {
      toast.style.display = 'none';
    }, 2500);
  }

  // 1. Load stored data
  const data = await chrome.storage.local.get(['label', 'token', 'email']);
  if (data.label) {
    labelInput.value = data.label;
  }

  // 2. Load email
  if (data.email) {
    emailVal.textContent = data.email;
  } else if (chrome.identity && chrome.identity.getProfileUserInfo) {
    chrome.identity.getProfileUserInfo({ accountStatus: 'ANY' }, (info) => {
      if (info && info.email) {
        emailVal.textContent = info.email;
        chrome.storage.local.set({ email: info.email });
      } else {
        emailVal.textContent = 'Chưa đăng nhập';
      }
    });
  } else {
    emailVal.textContent = 'Chưa đăng nhập';
  }

  // 3. Refresh status and active tabs
  function refreshStatus() {
    chrome.runtime.sendMessage({ type: 'getStatus' }, (resp) => {
      if (chrome.runtime.lastError || !resp) {
        connBadge.textContent = 'Offline';
        connBadge.className = 'badge badge-off';
        tabsVal.textContent = '0 tab';
        return;
      }
      if (resp.connected) {
        connBadge.textContent = 'Online';
        connBadge.className = 'badge badge-on';
      } else {
        connBadge.textContent = 'Offline';
        connBadge.className = 'badge badge-off';
      }
      if (resp.activeTabIds) {
        tabsVal.textContent = resp.activeTabIds.length + ' tab';
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
      showToast('Tên nhãn không hợp lệ (1-32 ký tự alphanumeric, ., _, -)', true);
      return;
    }

    await chrome.storage.local.set({ label: newLabel });
    chrome.runtime.sendMessage({ type: 'reconnect' }, () => {
      showToast('Đã lưu nhãn và kết nối lại!');
      setTimeout(refreshStatus, 500);
    });
  });

  labelInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      saveBtn.click();
    }
  });

  // 5. Open advanced options
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
      showToast('Đang kết nối lại...');
      setTimeout(refreshStatus, 500);
    });
  });
});
