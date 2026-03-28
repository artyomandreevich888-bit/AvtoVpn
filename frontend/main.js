(function() {
    const btn = document.getElementById('btn');
    const status = document.getElementById('status');
    const servers = document.getElementById('servers');
    const servicesEl = document.getElementById('services');
    const errorBox = document.getElementById('error-box');
    const errorText = document.getElementById('error-text');

    let connected = false;
    let polling = null;

    window.toggle = async function() {
        if (connected) {
            await disconnect();
        } else {
            await connect();
        }
    };

    window.copyError = async function() {
        try {
            await navigator.clipboard.writeText(errorText.textContent);
            errorBox.classList.add('copied');
            errorBox.querySelector('.error-hint').textContent = 'copied!';
            setTimeout(function() {
                errorBox.classList.remove('copied');
                errorBox.querySelector('.error-hint').textContent = 'click to copy';
            }, 2000);
        } catch (e) {}
    };

    async function connect() {
        hideError();
        setUI('connecting', 'CONNECTING', 'Downloading configs...');
        setServices([
            {Name: 'YouTube', Status: 'checking'},
            {Name: 'Instagram', Status: 'checking'},
            {Name: 'GitHub', Status: 'checking'}
        ]);
        try {
            const result = await window.go.main.App.Connect();
            if (result) {
                setUI('error', 'CONNECT', '');
                showError(result);
                resetServices();
                return;
            }
            connected = true;
            setUI('connected', 'DISCONNECT', 'Connected');
            startPolling();
            checkServices();
        } catch (e) {
            setUI('error', 'CONNECT', '');
            showError(e.toString());
            resetServices();
        }
    }

    async function disconnect() {
        try {
            await window.go.main.App.Disconnect();
        } catch (e) {}
        connected = false;
        stopPolling();
        setUI('disconnected', 'CONNECT', '');
        hideError();
        resetServices();
    }

    async function checkServices() {
        try {
            const checks = await window.go.main.App.CheckServices();
            setServices(checks);
        } catch (e) {}
    }

    function startPolling() {
        polling = setInterval(async () => {
            try {
                const s = await window.go.main.App.GetStatus();
                if (s.State === 'connected' && s.Server) {
                    status.textContent = s.Server + '  ' + s.Delay + 'ms';
                    servers.textContent = s.AliveCount + ' / ' + s.TotalCount + ' servers';
                } else if (s.State === 'error') {
                    setUI('error', 'CONNECT', '');
                    showError(s.Error);
                    connected = false;
                    stopPolling();
                    resetServices();
                }
            } catch (e) {}
        }, 3000);
    }

    function stopPolling() {
        if (polling) {
            clearInterval(polling);
            polling = null;
        }
    }

    function setUI(cls, label, statusText) {
        btn.className = 'btn ' + cls;
        btn.textContent = label;
        status.textContent = statusText;
        if (cls !== 'connected') servers.textContent = '';
    }

    function showError(msg) {
        errorText.textContent = msg;
        errorBox.style.display = 'block';
    }

    function hideError() {
        errorBox.style.display = 'none';
        errorText.textContent = '';
    }

    function setServices(checks) {
        servicesEl.innerHTML = checks.map(function(c) {
            var badge = c.Status;
            var label = '--';
            if (c.Status === 'ok') label = c.Delay + 'ms';
            else if (c.Status === 'fail') label = 'FAIL';
            else if (c.Status === 'checking') label = '...';
            return '<div class="svc">' +
                '<span class="svc-name">' + c.Name + '</span>' +
                '<span class="svc-badge ' + badge + '">' + label + '</span>' +
                '</div>';
        }).join('');
    }

    function resetServices() {
        setServices([
            {Name: 'YouTube', Status: 'idle'},
            {Name: 'Instagram', Status: 'idle'},
            {Name: 'GitHub', Status: 'idle'}
        ]);
    }
})();
