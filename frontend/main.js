(function() {
    const btn = document.getElementById('btn');
    const status = document.getElementById('status');
    const servers = document.getElementById('servers');

    let connected = false;
    let polling = null;

    window.toggle = async function() {
        if (connected) {
            await disconnect();
        } else {
            await connect();
        }
    };

    async function connect() {
        setUI('connecting', 'CONNECTING', 'Downloading configs...', '');
        try {
            const result = await window.go.main.App.Connect();
            if (result) {
                setUI('error', 'CONNECT', result, '');
                return;
            }
            connected = true;
            setUI('connected', 'DISCONNECT', 'Connected', '');
            startPolling();
        } catch (e) {
            setUI('error', 'CONNECT', e.toString(), '');
        }
    }

    async function disconnect() {
        try {
            await window.go.main.App.Disconnect();
        } catch (e) {}
        connected = false;
        stopPolling();
        setUI('disconnected', 'CONNECT', '', '');
    }

    function startPolling() {
        polling = setInterval(async () => {
            try {
                const s = await window.go.main.App.GetStatus();
                if (s.State === 'connected' && s.Server) {
                    status.textContent = s.Server + '  ' + s.Delay + 'ms';
                    servers.textContent = s.AliveCount + ' / ' + s.TotalCount + ' servers';
                } else if (s.State === 'error') {
                    setUI('error', 'CONNECT', s.Error, '');
                    connected = false;
                    stopPolling();
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

    function setUI(cls, label, statusText, serversText) {
        btn.className = 'btn ' + cls;
        btn.textContent = label;
        status.textContent = statusText;
        servers.textContent = serversText;
    }
})();
