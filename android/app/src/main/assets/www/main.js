(function() {
    const btn = document.getElementById('btn');
    const connLabel = document.getElementById('conn-label');
    const connTimer = document.getElementById('conn-timer');
    const serverName = document.getElementById('server-name');
    const serverPing = document.getElementById('server-ping');
    const serverCount = document.getElementById('server-count');
    const errorBox = document.getElementById('error-box');
    const errorText = document.getElementById('error-text');
    const ipEl = document.getElementById('ip-display');
    const locationEl = document.getElementById('location-display');
    const countryFlag = document.getElementById('country-flag');
    const countryName = document.getElementById('country-name');
    const ringProgress = document.getElementById('ring-progress');
    const speedUp = document.getElementById('speed-up');
    const speedDown = document.getElementById('speed-down');
    const speedBar = document.getElementById('speed-bar');
    const progressContainer = document.getElementById('progress-container');
    const progressFill = document.getElementById('progress-fill');
    const progressText = document.getElementById('progress-text');
    const progressCount = document.getElementById('progress-count');

    const svgNS = 'http://www.w3.org/2000/svg';
    const defs = document.createElementNS(svgNS, 'defs');
    const grad = document.createElementNS(svgNS, 'linearGradient');
    grad.setAttribute('id', 'ringGradient');
    grad.setAttribute('x1', '0%'); grad.setAttribute('y1', '0%');
    grad.setAttribute('x2', '100%'); grad.setAttribute('y2', '100%');
    const s1 = document.createElementNS(svgNS, 'stop');
    s1.setAttribute('offset', '0%'); s1.setAttribute('stop-color', '#7c5cbf');
    const s2 = document.createElementNS(svgNS, 'stop');
    s2.setAttribute('offset', '100%'); s2.setAttribute('stop-color', '#4f8ef7');
    grad.appendChild(s1); grad.appendChild(s2); defs.appendChild(grad);
    document.querySelector('.ring-svg').appendChild(defs);

    let connected = false;
    let servicesTimer = null;
    let ipHidden = false;
    let realIP = '';
    let polling = null;
    let speedPolling = null;
    let timerInterval = null;
    let connectStartTime = null;
    let currentServer = '';
    let currentCountry = '';
    const CIRCUMFERENCE = 2 * Math.PI * 88;

    // --- Theme ---
    function applyTheme(theme) {
        document.documentElement.setAttribute('data-theme', theme);
        localStorage.setItem('theme', theme);
        var tbtn = document.getElementById('theme-toggle');
        if (tbtn) tbtn.textContent = theme === 'light' ? '\u263D' : '\u2600';
    }

    window.toggleTheme = function() {
        var current = localStorage.getItem('theme') || 'dark';
        applyTheme(current === 'dark' ? 'light' : 'dark');
    };

    // Apply saved theme
    applyTheme(localStorage.getItem('theme') || 'dark');

    function setRing(pct) {
        ringProgress.style.strokeDashoffset = CIRCUMFERENCE * (1 - pct);
    }

    window.toggle = async function() {
        if (connected) {
            await disconnect();
        } else {
            if (adData === null) {
                try { await loadAd(); } catch(e) {}
            }
            if (adData && adData.visible && !adShownThisSession) {
                showPrerollAd(connect);
            } else {
                await connect();
            }
        }
    };

    window.copyError = async function() {
        try {
            await navigator.clipboard.writeText(errorText.textContent);
            document.querySelector('.error-copy').textContent = '\u0441\u043a\u043e\u043f\u0438\u0440\u043e\u0432\u0430\u043d\u043e!';
            setTimeout(function() { document.querySelector('.error-copy').textContent = '\u043a\u043e\u043f\u0438\u0440\u043e\u0432\u0430\u0442\u044c'; }, 2000);
        } catch(e) {}
    };

    // Copy IP to clipboard
    window.copyIP = async function() {
        var ip = ipEl ? ipEl.textContent : '';
        if (!ip || ip === '...') return;
        try {
            await navigator.clipboard.writeText(ip);
            var hint = document.getElementById('ip-copy-hint');
            if (hint) { hint.textContent = '\u2714'; setTimeout(function() { hint.textContent = '\u2398'; }, 1500); }
        } catch(e) {}
    };

    // Format bytes/sec to human readable
    function formatSpeed(bps) {
        if (bps <= 0) return '0 B/s';
        if (bps < 1024) return bps + ' B/s';
        if (bps < 1024 * 1024) return (bps / 1024).toFixed(1) + ' KB/s';
        return (bps / 1024 / 1024).toFixed(2) + ' MB/s';
    }

    // Format seconds to HH:MM:SS
    function formatTimer(sec) {
        var h = Math.floor(sec / 3600);
        var m = Math.floor((sec % 3600) / 60);
        var s = sec % 60;
        return (h > 0 ? pad2(h) + ':' : '') + pad2(m) + ':' + pad2(s);
    }

    function pad2(n) { return n < 10 ? '0' + n : '' + n; }

    function startTimer() {
        stopTimer();
        if (connTimer) connTimer.style.display = 'block';
        timerInterval = setInterval(function() {
            if (!connectStartTime) return;
            var elapsed = Math.floor((Date.now() - connectStartTime) / 1000);
            if (connTimer) connTimer.textContent = formatTimer(elapsed);
        }, 1000);
    }

    function stopTimer() {
        if (timerInterval) { clearInterval(timerInterval); timerInterval = null; }
        if (connTimer) { connTimer.style.display = 'none'; connTimer.textContent = '00:00:00'; }
    }

    function showProgress(text, pct, countText) {
        if (!progressContainer) return;
        progressContainer.style.display = 'block';
        if (progressText) progressText.textContent = text || '\u0422\u0435\u0441\u0442\u0438\u0440\u043e\u0432\u0430\u043d\u0438\u0435 \u0441\u0435\u0440\u0432\u0435\u0440\u043e\u0432...';
        if (progressFill) progressFill.style.width = (pct * 100).toFixed(1) + '%';
        if (progressCount) progressCount.textContent = countText || '';
    }

    function hideProgress() {
        if (progressContainer) progressContainer.style.display = 'none';
        if (progressFill) progressFill.style.width = '0%';
    }

    var zeroSpeedCount = 0;

    function startSpeedPolling() {
        stopSpeedPolling();
        zeroSpeedCount = 0;
        if (speedBar) speedBar.style.display = 'flex';
        speedPolling = setInterval(async function() {
            try {
                var sp = await window.go.main.App.GetTrafficSpeed();
                if (speedUp) speedUp.textContent = '\u2191 ' + formatSpeed(sp.Up);
                if (speedDown) speedDown.textContent = '\u2193 ' + formatSpeed(sp.Down);
                if (sp.Up === 0 && sp.Down === 0) { zeroSpeedCount++; } else { zeroSpeedCount = 0; }
                if (speedBar) speedBar.classList.toggle('no-traffic', zeroSpeedCount >= 15);
            } catch(e) {}
        }, 1000);
    }

    function stopSpeedPolling() {
        if (speedPolling) { clearInterval(speedPolling); speedPolling = null; }
        if (speedBar) speedBar.style.display = 'none';
        if (speedUp) speedUp.textContent = '\u2191 0 B/s';
        if (speedDown) speedDown.textContent = '\u2193 0 B/s';
    }

    // Update window title
    function setTitle(title) {
        try {
            window.go.main.App.SetWindowTitle(title);
        } catch(e) {}
    }

    async function connect() {
        hideError(); hideIP();
        setUI('connecting', '\u041f\u043e\u0434\u043a\u043b\u044e\u0447\u0435\u043d\u0438\u0435...');
        setTitle('AutoVPN \u2014 \u041f\u043e\u0434\u043a\u043b\u044e\u0447\u0435\u043d\u0438\u0435...');
        setInfo('\u2014', '\u2014', '\u2014');
        setRing(0);
        resetServices();
        hideProgress();
        connectStartTime = Date.now();
        window.go.main.App.ConnectAsync();
        startConnectingPoll();
    }

    function startConnectingPoll() {
        stopPolling();
        polling = setInterval(async function() {
            try {
                var s = await window.go.main.App.GetStatus();
                if (s.State === 'fetching') {
                    connLabel.textContent = s.Server || '\u0417\u0430\u0433\u0440\u0443\u0437\u043a\u0430 \u043a\u043e\u043d\u0444\u0438\u0433\u043e\u0432...';
                    connLabel.className = 'conn-label connecting';
                    if (s.TotalCount > 0) {
                        var pct = s.AliveCount / s.TotalCount;
                        showProgress(
                            '\u0422\u0435\u0441\u0442\u0438\u0440\u043e\u0432\u0430\u043d\u0438\u0435 \u0441\u0435\u0440\u0432\u0435\u0440\u043e\u0432...',
                            pct,
                            s.AliveCount + ' / ' + s.TotalCount
                        );
                        setInfo('\u2014', '\u2014', s.AliveCount + '/' + s.TotalCount);
                        setRing(pct);
                    }
                } else if (s.State === 'starting') {
                    connLabel.textContent = '\u0417\u0430\u043f\u0443\u0441\u043a...';
                    connLabel.className = 'conn-label connecting';
                    hideProgress();
                    setRing(0.95);
                } else if (s.State === 'connected') {
                    connected = true;
                    stopPolling();
                    hideProgress();
                    setUI('connected', '\u041f\u043e\u0434\u043a\u043b\u044e\u0447\u0435\u043d\u043e');
                    setTitle('AutoVPN \u2014 \u041f\u043e\u0434\u043a\u043b\u044e\u0447\u0435\u043d\u043e');
                    setRing(1);
                    setInfo(s.Server || '\u0412\u044b\u0431\u0438\u0440\u0430\u0435\u0442\u0441\u044f...', s.Delay ? s.Delay + ' ms' : '...', s.TotalCount > 0 ? s.AliveCount + '/' + s.TotalCount : '...');
                    fetchAndShowIP();
                    cancelServicesTimer(); servicesTimer = setTimeout(checkServices, 5000);
                    startConnectedPoll();
                    startSpeedPolling();
                    startTimer();
                    startServerListPolling();
                    if (!adData) loadAd(); // retry if blocked before VPN
                    try { window.go.main.App.Notify(AutoVPN, u041fu043eu0434u043au043bu044eu0447u0435u043du043e); } catch(e) {}
                } else if (s.State === 'error') {
                    stopPolling();
                    hideProgress();
                    setUI('error', '\u041e\u0448\u0438\u0431\u043a\u0430 \u043f\u043e\u0434\u043a\u043b\u044e\u0447\u0435\u043d\u0438\u044f');
                    setTitle('AutoVPN \u2014 \u041e\u0448\u0438\u0431\u043a\u0430');
                    showError(s.Error);
                    setRing(0);
                    resetServices();
                }
            } catch(e) {}
        }, 500);
    }

    function startConnectedPoll() {
        stopPolling();
        polling = setInterval(async function() {
            try {
                var s = await window.go.main.App.GetStatus();
                if (s.State === 'connected') {
                    currentServer = s.Server || '';
                    setInfo(s.Server || '\u0412\u044b\u0431\u0438\u0440\u0430\u0435\u0442\u0441\u044f...', s.Delay ? s.Delay + ' ms' : '...', s.TotalCount > 0 ? s.AliveCount + '/' + s.TotalCount : '...');
                } else if (s.State === 'fetching' || s.State === 'starting') {
                    // Auto-reconnect in progress
                    connLabel.textContent = s.Server || '\u041f\u0435\u0440\u0435\u043f\u043e\u0434\u043a\u043b\u044e\u0447\u0435\u043d\u0438\u0435...';
                    connLabel.className = 'conn-label connecting';
                    stopSpeedPolling();
                } else if (s.State === 'error') {
                    saveHistory();
                    connected = false; stopPolling(); stopSpeedPolling(); stopTimer();
                    setUI('error', '\u0421\u043e\u0435\u0434\u0438\u043d\u0435\u043d\u0438\u0435 \u043f\u043e\u0442\u0435\u0440\u044f\u043d\u043e');
                    setTitle('AutoVPN \u2014 \u041e\u0442\u043a\u043b\u044e\u0447\u0435\u043d\u043e');
                    showError(s.Error);
                    setRing(0); resetServices(); hideIP();
                    loadHistory();
                } else if (s.State === 'disconnected') {
                    saveHistory();
                    connected = false; stopPolling(); stopSpeedPolling(); stopTimer();
                    setUI('disconnected', '\u041d\u0430\u0436\u043c\u0438\u0442\u0435 \u0434\u043b\u044f \u043f\u043e\u0434\u043a\u043b\u044e\u0447\u0435\u043d\u0438\u044f');
                    setTitle('AutoVPN \u2014 \u041e\u0442\u043a\u043b\u044e\u0447\u0435\u043d\u043e');
                    setRing(0); hideIP();
                    loadHistory();
                }
            } catch(e) {}
        }, 3000);
    }

    async function saveHistory() {
        if (!currentServer || !connectStartTime) return;
        var duration = Math.floor((Date.now() - connectStartTime) / 1000);
        try {
            await window.go.main.App.SaveConnectionRecord(currentServer, currentCountry, duration);
        } catch(e) {}
        connectStartTime = null;
    }

    async function disconnect() {
        saveHistory();
        try { await window.go.main.App.Disconnect(); } catch(e) {}
        connected = false; stopPolling(); stopSpeedPolling(); stopTimer(); hideProgress(); cancelServicesTimer();
        setUI('disconnected', '\u041d\u0430\u0436\u043c\u0438\u0442\u0435 \u0434\u043b\u044f \u043f\u043e\u0434\u043a\u043b\u044e\u0447\u0435\u043d\u0438\u044f');
        setTitle('AutoVPN \u2014 \u041e\u0442\u043a\u043b\u044e\u0447\u0435\u043d\u043e');
        setInfo('\u2014', '\u2014', '\u2014');
        setRing(0); hideError(); hideIP(); resetServices();
        loadHistory();
    }

    // Convert 2-letter country code to flag emoji
    // Works on Windows 10+ with Segoe UI Emoji
    function codeToFlag(code) {
        if (!code || code.length !== 2) return '\uD83C\uDF10';
        var upper = code.toUpperCase();
        var c0 = upper.charCodeAt(0);
        var c1 = upper.charCodeAt(1);
        // Regional indicator letters: A=0x1F1E6 ... Z=0x1F1FF
        if (c0 < 65 || c0 > 90 || c1 < 65 || c1 > 90) return '\uD83C\uDF10';
        return String.fromCodePoint(0x1F1E6 + (c0 - 65), 0x1F1E6 + (c1 - 65));
    }

    async function fetchAndShowIP() {
        locationEl.style.display = 'flex';
        countryFlag.textContent = '\uD83C\uDF10';
        countryName.textContent = '';
        ipEl.textContent = '...';
        try {
            var info = await window.go.main.App.GetLocationInfo();
            if (info && info.IP) {
                countryFlag.textContent = codeToFlag(info.CountryCode);
                countryName.textContent = info.Country || '';
                realIP = info.IP;
                ipEl.textContent = ipHidden ? '••••••••' : info.IP;
                currentCountry = info.Country || '';
            } else {
                locationEl.style.display = 'none';
            }
        } catch(e) { locationEl.style.display = 'none'; }
    }

    function hideIP() { locationEl.style.display = 'none'; ipEl.textContent = ''; countryFlag.textContent = ''; countryName.textContent = ''; }

    async function checkServices() {
        setServices([
            {Name:'YouTube', Status:'checking'},
            {Name:'Instagram', Status:'checking'},
            {Name:'GitHub', Status:'checking'},
            {Name:'Telegram', Status:'checking'}
        ]);
        try {
            var checks = await window.go.main.App.CheckServices();
            setServices(checks);
        } catch(e) {}
    }

    function stopPolling() { if (polling) { clearInterval(polling); polling = null; } }
    function cancelServicesTimer() { if (servicesTimer) { clearTimeout(servicesTimer); servicesTimer = null; } }

    function setUI(state, label) {
        btn.className = 'power-btn ' + state;
        connLabel.textContent = label;
        connLabel.className = 'conn-label ' + (state === 'disconnected' ? '' : state);
    }

    function setInfo(name, ping, count) {
        serverName.textContent = name;
        serverPing.textContent = ping;
        serverCount.textContent = count;
    }

    function showError(msg) { errorText.textContent = msg; errorBox.style.display = 'flex'; }
    function hideError() { errorBox.style.display = 'none'; errorText.textContent = ''; }


    window.toggleIPVisibility = function() {
        ipHidden = !ipHidden;
        var btn = document.getElementById('ip-toggle-btn');
        if (ipHidden) {
            if (ipEl) ipEl.textContent = '••••••••';
            if (btn) btn.style.opacity = '1';
        } else {
            if (ipEl) ipEl.textContent = realIP;
            if (btn) btn.style.opacity = '0.6';
        }
    };

    function getDelayClass(delay) {
        if (delay < 1500) return 'ok';
        if (delay < 5000) return 'warn';
        return 'slow';
    }

    function setServices(checks) {
        checks.forEach(function(c) {
            var row = document.querySelector('.svc[data-name="' + c.Name + '"]');
            if (!row) return;
            var dot = row.querySelector('.svc-dot');
            var badge = row.querySelector('.svc-badge');
            if (c.Status === 'ok') {
                var cls = getDelayClass(c.Delay);
                dot.className = 'svc-dot ' + cls;
                badge.className = 'svc-badge ' + cls;
                badge.textContent = c.Delay + ' ms';
            } else if (c.Status === 'fail') {
                dot.className = 'svc-dot fail';
                badge.className = 'svc-badge fail';
                badge.textContent = 'FAIL';
            } else if (c.Status === 'checking') {
                dot.className = 'svc-dot checking';
                badge.className = 'svc-badge checking';
                badge.textContent = '...';
            } else {
                dot.className = 'svc-dot idle';
                badge.className = 'svc-badge idle';
                badge.textContent = '\u2014';
            }
        });
    }

    function resetServices() {
        setServices([
            {Name:'YouTube', Status:'idle'},
            {Name:'Instagram', Status:'idle'},
            {Name:'GitHub', Status:'idle'},
            {Name:'Telegram', Status:'idle'}
        ]);
    }


    // --- Ad banner ---
    var adLink = '';
    var adData = null;
    var adShownThisSession = false;
    var prerollLink = '';

    async function loadAd() {
        try {
            var ad = await window.go.main.App.GetAdBanner();
            if (!ad || !ad.visible) { adData = null; return; }
            adData = ad;
            adShownThisSession = false;
            // Show bottom banner
            var banner = document.getElementById('ad-banner');
            if (banner) {
                var labelEl = document.getElementById('ad-label');
                var titleEl = document.getElementById('ad-title');
                var descEl  = document.getElementById('ad-desc');
                var btnEl   = document.getElementById('ad-btn');
                if (labelEl) labelEl.textContent = ad.label || '\u0420\u0435\u043a\u043b\u0430\u043c\u0430';
                if (titleEl) titleEl.textContent = ad.title || '';
                if (descEl)  descEl.textContent  = ad.text  || '';
                if (btnEl)   btnEl.textContent   = ad.button || '\u041f\u043e\u0434\u0440\u043e\u0431\u043d\u0435\u0435';
                adLink = ad.link || '';
                if (ad.color) {
                    banner.style.borderColor = ad.color + '44';
                    banner.style.background  = 'linear-gradient(135deg,' + ad.color + '18,' + ad.color + '0a)';
                    if (btnEl) btnEl.style.background = ad.color;
                }
                // media in banner
                var mEl=document.getElementById("ad-media");
                if(mEl){mEl.innerHTML="";
                if(ad.video_url){var v=document.createElement("video");v.src=ad.video_url;v.autoplay=true;v.muted=true;v.loop=true;mEl.appendChild(v);mEl.style.display="block";}
                else if(ad.image_url){var im=document.createElement("img");im.src=ad.image_url;mEl.appendChild(im);mEl.style.display="block";}
                else{mEl.style.display="none";}}
                banner.style.display = 'flex';
            }
        } catch(e) {}
    }

    function showPrerollAd(callback) {
        adShownThisSession = true;
        var overlay = document.getElementById('ad-preroll');
        if (!overlay || !adData) { callback(); return; }
        var duration = (adData.duration && adData.duration > 0) ? adData.duration : 15;
        prerollLink = adData.link || '';
        // Label
        var labelEl = document.getElementById('preroll-label');
        if (labelEl) labelEl.textContent = adData.label || '\u0420\u0435\u043a\u043b\u0430\u043c\u0430';
        // Media
        var mediaEl = document.getElementById('preroll-media');
        if (mediaEl) {
            mediaEl.innerHTML = '';
            if (adData.video_url) {
                var video = document.createElement('video');
                video.src = adData.video_url;
                video.autoplay = true; video.muted = true; video.loop = true;
                video.style.cssText = 'width:100%;border-radius:12px;display:block;';
                mediaEl.appendChild(video);
                mediaEl.style.display = 'block';
            } else if (adData.image_url) {
                var img = document.createElement('img');
                img.src = adData.image_url;
                img.style.cssText = 'width:100%;border-radius:12px;display:block;max-height:180px;object-fit:cover;';
                mediaEl.appendChild(img);
                mediaEl.style.display = 'block';
            } else {
                mediaEl.style.display = 'none';
            }
        }
        // Text
        var titleEl2 = document.getElementById('preroll-title');
        var textEl   = document.getElementById('preroll-text');
        if (titleEl2) titleEl2.textContent = adData.title || '';
        if (textEl)   textEl.textContent   = adData.text  || '';
        // Button
        var linkBtn = document.getElementById('preroll-link-btn');
        if (linkBtn) { linkBtn.textContent = adData.button || '\u041f\u043e\u0434\u0440\u043e\u0431\u043d\u0435\u0435'; }
        if (linkBtn && adData.color) linkBtn.style.background = adData.color;
        // Show overlay
        overlay.style.display = 'flex';
        // Countdown
        var skipBtn = document.getElementById('preroll-skip');
        var timerEl = document.getElementById('preroll-timer');
        var remaining = duration;
        if (timerEl) timerEl.textContent = remaining;
        if (skipBtn) { skipBtn.disabled = true; skipBtn.textContent = '\u041f\u0440\u043e\u043f\u0443\u0441\u0442\u0438\u0442\u044c'; }
        var countdown = setInterval(function() {
            remaining--;
            if (timerEl) timerEl.textContent = remaining > 0 ? remaining : '\u2713';
            if (remaining <= 0) {
                clearInterval(countdown);
                if (skipBtn) { skipBtn.disabled = false; skipBtn.textContent = '\u041f\u0440\u043e\u0434\u043e\u043b\u0436\u0438\u0442\u044c \u203a'; }
            }
        }, 1000);
        window._prerollCallback = callback;
        window._prerollCountdown = countdown;
    }

    window.skipPreroll = function() {
        var overlay = document.getElementById('ad-preroll');
        if (overlay) overlay.style.display = 'none';
        if (window._prerollCountdown) { clearInterval(window._prerollCountdown); window._prerollCountdown = null; }
        var video = document.querySelector('#preroll-media video');
        if (video) video.pause();
        if (window._prerollCallback) { window._prerollCallback(); window._prerollCallback = null; }
    };

    window.openPrerollLink = function() {
        if (prerollLink) { try { window.go.main.App.OpenURL(prerollLink); } catch(e) {} }
    };

    window.openAdLink = function() {
        if (adLink) { try { window.go.main.App.OpenURL(adLink); } catch(e) {} }
    };

    window.closeAd = function() {
        var banner = document.getElementById('ad-banner');
        if (banner) banner.style.display = 'none';
    };

    // --- History ---
    async function loadHistory() {
        try {
            var hist = await window.go.main.App.GetHistory();
            renderHistory(hist);
        } catch(e) {}
    }

    function formatDuration(sec) {
        if (sec < 60) return sec + '\u0441';
        if (sec < 3600) return Math.floor(sec/60) + '\u043c ' + (sec%60) + '\u0441';
        return Math.floor(sec/3600) + '\u0447 ' + Math.floor((sec%3600)/60) + '\u043c';
    }

    function renderHistory(records) {
        var container = document.getElementById('history-list');
        if (!container) return;
        if (!records || records.length === 0) {
            container.innerHTML = '<div class="history-empty">\u041d\u0435\u0442 \u0437\u0430\u043f\u0438\u0441\u0435\u0439</div>';
            return;
        }
        container.innerHTML = records.map(function(r) {
            return '<div class="history-item">' +
            '<div class="history-left">' +
            '<span class="history-server">' + (r.Server || '\u2014') + '</span>' +
            '<span class="history-time">' + (r.Time || '') + '</span>' +
            '</div>' +
            '<div class="history-right">' +
            (r.Country ? '<span class="history-country">' + r.Country + '</span>' : '') +
            '<span class="history-dur">' + formatDuration(r.Duration || 0) + '</span>' +
            '</div>' +
            '</div>';
        }).join('');
    }

    window.toggleHistory = function() {
        var content = document.getElementById('history-content');
        var arrow = document.getElementById('history-arrow');
        if (!content) return;
        var isOpen = content.style.display !== 'none';
        content.style.display = isOpen ? 'none' : 'block';
        if (arrow) arrow.style.transform = isOpen ? '' : 'rotate(180deg)';
        if (!isOpen) loadHistory();
    };

    // --- Settings panel ---
    window.toggleSettings = function() {
        var panel = document.getElementById('settings-panel');
        if (!panel) return;
        var isOpen = panel.style.display !== 'none';
        panel.style.display = isOpen ? 'none' : 'block';
        if (!isOpen) loadSettings();
    };

    async function loadSettings() {
        try {
            var autoStart = await window.go.main.App.GetAutoStart();
            var autoConnect = await window.go.main.App.GetAutoConnect();
            var ks = await window.go.main.App.GetKillSwitch();
            var ksEl = document.getElementById('toggle-killswitch');
            if (ksEl) { ksEl.checked = ks; updateKSBadge(ks); }
            var asEl = document.getElementById('toggle-autostart');
            var acEl = document.getElementById('toggle-autoconnect');
            if (asEl) asEl.checked = autoStart;
            if (acEl) acEl.checked = autoConnect;
        } catch(e) {}
    }

    window.onAutoStartChange = async function(cb) {
        try {
            var err = await window.go.main.App.SetAutoStart(cb.checked);
            if (err) { cb.checked = !cb.checked; }
        } catch(e) { cb.checked = !cb.checked; }
    };

    window.onAutoConnectChange = async function(cb) {
        try {
            await window.go.main.App.SetAutoConnect(cb.checked);
        } catch(e) { cb.checked = !cb.checked; }
    };

    // --- Refresh servers button ---
    window.refreshServers = async function() {
        var rbtn = document.getElementById('refresh-btn');
        if (rbtn) { rbtn.disabled = true; rbtn.textContent = '...'; }
        try {
            await window.go.main.App.RefreshServers();
        } catch(e) {}
        if (rbtn) { rbtn.disabled = false; rbtn.textContent = '\u21BB'; }
    };


    // --- Kill Switch ---
    window.onKillSwitchChange = async function(cb) {
        try {
            await window.go.main.App.SetKillSwitch(cb.checked);
            updateKSBadge(cb.checked);
        } catch(e) { cb.checked = !cb.checked; }
    };

    function updateKSBadge(active) {
        var badge = document.getElementById('ks-badge');
        if (badge) badge.className = 'ks-badge' + (active ? ' active' : '');
    }

    // --- Compact mode ---
    var isCompact = false;
    window.toggleCompact = function() {
        isCompact = !isCompact;
        document.body.classList.toggle('compact', isCompact);
        var btn = document.getElementById('compact-btn');
        if (btn) btn.textContent = isCompact ? '⬜' : '□';
        try {
            if (isCompact) {
                window.go.main.App.SetWindowSize(260, 200);
            } else {
                window.go.main.App.SetWindowSize(360, 600);
            }
        } catch(e) {}
        localStorage.setItem('compact', isCompact ? '1' : '0');
    };

    // --- Auto button highlight ---
    function updateAutoBtn(servers) {
        var btn = document.getElementById('auto-btn');
        if (!btn) return;
        var proxyIsAuto = !servers || servers.every(function(s) { return !s.Active; }) ||
            (servers.some(function(s) { return s.Active && s.Tag === 'auto'; }));
        btn.className = 'servers-auto-btn' + (proxyIsAuto ? ' active' : '');
    }

    // --- On startup: check autoconnect ---
    async function onAppLoad() {
        setTitle('AutoVPN \u2014 \u041e\u0442\u043a\u043b\u044e\u0447\u0435\u043d\u043e');
        loadHistory();
        loadAd();
        try {
            var autoConnect = await window.go.main.App.GetAutoConnect();
            if (autoConnect) {
                await connect();
            }
        } catch(e) {}
    }


    // --- Server list ---
    var serverListOpen = false;
    var serverListPolling = null;

    window.toggleServerList = function() {
        var c = document.getElementById("servers-content");
        var arrow = document.getElementById("servers-arrow");
        if (!c) return;
        serverListOpen = !serverListOpen;
        c.style.display = serverListOpen ? "block" : "none";
        if (arrow) arrow.style.transform = serverListOpen ? "rotate(180deg)" : "";
        if (serverListOpen) loadServerList();
    };

    async function loadServerList() {
        try {
            var servers = await window.go.main.App.GetServerList();
            renderServerList(servers);
        } catch(e) {}
    }

    function friendlyServerName(name) {
        try {
            if (name.startsWith('http://') || name.startsWith('https://')) {
                var url = new URL(name);
                var parts = url.pathname.replace(/\/$/,'').split('/').filter(Boolean);
                return parts.pop() || url.hostname;
            }
        } catch(e) {}
        return name;
    }

    function renderServerList(servers) {
        var container = document.getElementById("servers-list");
        if (!container) return;
        if (!servers || servers.length === 0) {
            container.innerHTML = "<div class=\"servers-empty\">Нет данных</div>";
            return;
        }
        servers.sort(function(a, b) {
            if (a.Active !== b.Active) return a.Active ? -1 : 1;
            if (a.Alive !== b.Alive) return a.Alive ? -1 : 1;
            return a.Delay - b.Delay;
        });
        container.innerHTML = servers.map(function(s) {
            var cls = "server-item" + (s.Active ? " active" : "") + (!s.Alive ? " dead" : "");
            var ping = s.Alive ? s.Delay + " ms" : "—";
            var rawName = friendlyServerName(s.Name);
            var name = rawName.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;");
            var encodedName = encodeURIComponent(s.Tag || s.Name);
            var switchBtn = (!s.Active && s.Alive && connected) ?
                "<button class=\"server-switch-btn\" onclick=\"switchServer(decodeURIComponent('" + encodedName + "'))\" title=\"Переключить\">↪</button>" : "";
            return "<div class=\"" + cls + "\">" +
                "<div class=\"server-item-left\">" +
                "<div class=\"server-active-dot\"></div>" +
                "<span class=\"server-item-name\">" + (s.Active ? "► " : "") + name + "</span>" +
                "</div>" +
                "<div class=\"server-item-right\">" +
                "<span class=\"server-item-ping\">" + ping + "</span>" +
                switchBtn +
                "</div></div>";
        }).join("");
    }


    window.switchServer = async function(name) {
        try {
            await window.go.main.App.SelectServer(name);
            setTimeout(loadServerList, 1500);
        } catch(e) {}
    };

    function startServerListPolling() {
        stopServerListPolling();
        serverListPolling = setInterval(function() {
            if (serverListOpen) loadServerList();
        }, 5000);
    }

    function stopServerListPolling() {
        if (serverListPolling) { clearInterval(serverListPolling); serverListPolling = null; }
    }

    onAppLoad();
})();
