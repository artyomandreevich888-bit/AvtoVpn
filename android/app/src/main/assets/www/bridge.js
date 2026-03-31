// Android bridge: maps window.go.main.App.* to window.AndroidBridge.*
(function() {
    var B = window.AndroidBridge;
    if (!B) { console.error("AndroidBridge not found"); return; }

    function wrap(fn) {
        return function() {
            var args = arguments;
            return new Promise(function(resolve, reject) {
                try { resolve(fn.apply(null, args)); }
                catch(e) { reject(e); }
            });
        };
    }

    function parseJSON(s) {
        try { return s ? JSON.parse(s) : null; } catch(e) { return null; }
    }

    // Async pattern for blocking calls: trigger + poll
    function asyncCall(trigger, pollFn, interval) {
        interval = interval || 400;
        return new Promise(function(resolve) {
            trigger();
            var t = setInterval(function() {
                var r = pollFn();
                if (r !== null && r !== undefined && r !== "") {
                    clearInterval(t);
                    resolve(r);
                }
            }, interval);
        });
    }

    window.go = { main: { App: {
        ConnectAsync: function() {
            B.connectAsync();
            return Promise.resolve();
        },
        Disconnect: wrap(function() { B.disconnect(); }),
        GetStatus: wrap(function() {
            return parseJSON(B.getStatusJSON()) || {State:"disconnected",Server:"",Delay:0,AliveCount:0,TotalCount:0,Error:""};
        }),
        GetTrafficSpeed: wrap(function() {
            var parts = (B.getTraffic()||"0,0").split(",");
            return {Up: parseInt(parts[0])||0, Down: parseInt(parts[1])||0};
        }),
        GetLocationInfo: function() {
            return asyncCall(
                function() { B.triggerLocationInfo(); },
                function() { return parseJSON(B.getLocationInfoResult()); }
            );
        },
        CheckServices: function() {
            return asyncCall(
                function() { B.triggerCheckServices(); },
                function() { return parseJSON(B.getServicesResult()); }
            );
        },
        GetServerList: wrap(function() {
            return parseJSON(B.getServerListJSON()) || [];
        }),
        GetHistory: wrap(function() {
            return parseJSON(B.getHistoryJSON()) || [];
        }),
        SaveConnectionRecord: wrap(function(server, country, duration) {
            B.saveRecord(server, country, duration);
        }),
        GetAutoConnect: wrap(function() { return B.getAutoConnect(); }),
        SetAutoConnect: wrap(function(v) { B.setAutoConnect(v); return null; }),
        GetAutoStart: wrap(function() { return false; }),
        SetAutoStart: wrap(function(v) { return null; }),
        GetKillSwitch: wrap(function() { return window.AndroidBridge ? window.AndroidBridge.getKillSwitch() : false; }),
        SetKillSwitch: wrap(function(v) { if (window.AndroidBridge) window.AndroidBridge.setKillSwitch(!!v); return null; }),
        SelectServer: function(name) {
            return asyncCall(
                function() { B.triggerSelectServer(name); },
                function() { return B.getSelectServerDone() ? "done" : ""; }
            );
        },
        RefreshServers: wrap(function() { B.refreshServers(); }),
        OpenURL: wrap(function(url) { B.openUrl(url); }),
        Notify: wrap(function(t, m) {}),
        SetWindowTitle: wrap(function() {}),
        SetWindowSize: wrap(function() {}),
        GetAdBanner: function() {
            return asyncCall(
                function() { B.triggerAdBanner(); },
                function() {
                    var r = B.getAdBannerResult();
                    // return empty string while pending, null if no ad, JSON if ad
                    return r === "__pending__" ? "" : (r || "__null__");
                }
            ).then(function(r) { return r === "__null__" ? null : JSON.parse(r); });
        },
    }}};

    // Integrity check — if files were tampered, force ad reload
    try {
        var ok = B.checkIntegrity();
        if (!ok) {
            // Files modified — override closeAd to prevent disabling ads
            window.closeAd = function() {};
            window.adIntegrityFailed = true;
        }
    } catch(e) {}

})();


// Server-side app check
(function() {
    if (typeof window.AndroidBridge !== "undefined" && window.AndroidBridge.checkAppAllowed) {
        try {
            var result = JSON.parse(window.AndroidBridge.checkAppAllowed());
            if (result && result.allowed === false) {
                document.body.innerHTML = "<div style=\"display:flex;align-items:center;justify-content:center;height:100vh;background:#0a0a1a;color:#fff;font-family:sans-serif;text-align:center;padding:20px\"><div><h2 style=\"color:#f55\">Приложение заблокировано</h2><p>" + (result.message || "Обратитесь в поддержку") + "</p></div></div>";
            }
        } catch(e) {}
    }
})();
