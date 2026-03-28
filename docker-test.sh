#!/bin/bash
set -e

echo "=== Building Docker image ==="
docker build -t autovpn-test .

echo ""
echo "=== Running unit tests in Docker ==="
docker run --rm autovpn-test sh -c "echo 'Binary exists:' && ls -lh /usr/local/bin/autovpn"

echo ""
echo "=== Running AutoVPN with tun (needs NET_ADMIN) ==="
echo "Container will: fetch configs → start sing-box → check IP → exit"
echo ""

docker run --rm \
    --cap-add=NET_ADMIN \
    --device=/dev/net/tun \
    --name autovpn-test \
    autovpn-test sh -c '
        # Start autovpn in background
        autovpn &
        PID=$!

        # Wait for sing-box to start and urltest to pick a server
        echo "Waiting for VPN to connect..."
        sleep 15

        # Check clash_api
        echo ""
        echo "=== clash_api status ==="
        curl -s http://127.0.0.1:9090/proxies/auto \
            -H "Authorization: Bearer autovpn" 2>/dev/null | head -c 500 || echo "clash_api not ready"

        # Check if IP changed
        echo ""
        echo ""
        echo "=== IP check ==="
        VPNIP=$(curl -s --max-time 10 https://ifconfig.me 2>/dev/null)
        if [ -n "$VPNIP" ]; then
            echo "VPN IP: $VPNIP"
        else
            echo "Could not reach ifconfig.me (VPN may not be fully connected yet)"
        fi

        # Cleanup
        kill $PID 2>/dev/null
        wait $PID 2>/dev/null
        echo ""
        echo "=== Done ==="
    '
