package mobile

import (
	"fmt"
	"log"
	"net"
	"os"
	"syscall"
	"unsafe"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
	"golang.org/x/sys/unix"
)

// androidPlatform implements adapter.PlatformInterface for Android.
// It delegates TUN creation and socket protection to the Kotlin VpnService.
type androidPlatform struct {
	vpn            VPNService
	tunFd          int
	netIfName      string
	netIfIndex     int
	networkManager adapter.NetworkManager
}

var _ adapter.PlatformInterface = (*androidPlatform)(nil)

func (p *androidPlatform) Initialize(nm adapter.NetworkManager) error {
	p.networkManager = nm
	return nil
}

func (p *androidPlatform) UsePlatformAutoDetectInterfaceControl() bool {
	return true
}

func (p *androidPlatform) AutoDetectInterfaceControl(fd int) error {
	ok := p.vpn.Protect(int32(fd))
	log.Printf("[autovpn] protect fd=%d ok=%v", fd, ok)
	if ok {
		return nil
	}
	return os.ErrPermission
}

func (p *androidPlatform) UsePlatformInterface() bool {
	return true
}

func (p *androidPlatform) OpenInterface(options *tun.Options, _ option.TunPlatformOptions) (tun.Tun, error) {
	log.Printf("[autovpn] OpenInterface: tunFd=%d, MTU=%d", p.tunFd, options.MTU)
	name, err := getTunName(p.tunFd)
	if err != nil {
		log.Printf("[autovpn] OpenInterface: getTunName failed: %v, using 'tun0'", err)
		name = "tun0"
	}
	log.Printf("[autovpn] OpenInterface: name=%q, autoRoute=%v", name, options.AutoRoute)
	options.Name = name

	if options.InterfaceMonitor != nil {
		options.InterfaceMonitor.RegisterMyInterface(name)
	}

	dupFd, err := syscall.Dup(p.tunFd)
	if err != nil {
		return nil, fmt.Errorf("dup fd: %w", err)
	}
	log.Printf("[autovpn] OpenInterface: dupFd=%d, creating tun.New", dupFd)
	options.FileDescriptor = dupFd
	t, err := tun.New(*options)
	if err != nil {
		log.Printf("[autovpn] OpenInterface: tun.New FAILED: %v", err)
		return nil, err
	}
	log.Printf("[autovpn] OpenInterface: OK")
	return t, nil
}

func getTunName(fd int) (string, error) {
	var ifr [unix.IFNAMSIZ + 64]byte
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.TUNGETIFF),
		uintptr(unsafe.Pointer(&ifr[0])),
	)
	if errno != 0 {
		return "", fmt.Errorf("TUNGETIFF: %w", errno)
	}
	return unix.ByteSliceToString(ifr[:]), nil
}

func (p *androidPlatform) UsePlatformDefaultInterfaceMonitor() bool {
	return false
}

func (p *androidPlatform) CreateDefaultInterfaceMonitor(logger.Logger) tun.DefaultInterfaceMonitor {
	return &androidInterfaceMonitor{platform: p, netIfName: p.netIfName, netIfIndex: p.netIfIndex}
}

func (p *androidPlatform) UsePlatformNetworkInterfaces() bool {
	return true
}

func (p *androidPlatform) NetworkInterfaces() ([]adapter.NetworkInterface, error) {
	if p.netIfName == "" {
		return nil, nil
	}
	return []adapter.NetworkInterface{
		{
			Interface: control.Interface{
				Index: p.netIfIndex,
				Name:  p.netIfName,
				MTU:   1500,
				Flags: net.FlagUp | net.FlagRunning | net.FlagMulticast,
			},
			Type: 1, // C.InterfaceTypeWIFI
		},
	}, nil
}

func (p *androidPlatform) UnderNetworkExtension() bool {
	return false
}

func (p *androidPlatform) NetworkExtensionIncludeAllNetworks() bool {
	return false
}

func (p *androidPlatform) ClearDNSCache() {}

func (p *androidPlatform) RequestPermissionForWIFIState() error {
	return nil
}

func (p *androidPlatform) UsePlatformWIFIMonitor() bool {
	return false
}

func (p *androidPlatform) ReadWIFIState() adapter.WIFIState {
	return adapter.WIFIState{}
}

func (p *androidPlatform) SystemCertificates() []string {
	return nil
}

func (p *androidPlatform) UsePlatformConnectionOwnerFinder() bool {
	return false
}

func (p *androidPlatform) FindConnectionOwner(*adapter.FindConnectionOwnerRequest) (*adapter.ConnectionOwner, error) {
	return nil, os.ErrInvalid
}

func (p *androidPlatform) UsePlatformNotification() bool {
	return false
}

func (p *androidPlatform) SendNotification(*adapter.Notification) error {
	return nil
}

// androidInterfaceMonitor provides the default network interface for outbound connections.
type androidInterfaceMonitor struct {
	platform   *androidPlatform
	netIfName  string
	netIfIndex int
	defaultIf  *control.Interface
	myInterface string
	callbacks   list.List[tun.DefaultInterfaceUpdateCallback]
}

func (m *androidInterfaceMonitor) Start() error {
	m.detectDefault()
	// Populate NetworkManager's interface list and fire callback
	if m.defaultIf != nil && m.platform.networkManager != nil {
		if err := m.platform.networkManager.UpdateInterfaces(); err != nil {
			log.Printf("[autovpn] UpdateInterfaces failed: %v", err)
		}
		for _, cb := range m.callbacks.Array() {
			cb(m.defaultIf, 0)
		}
	}
	return nil
}

func (m *androidInterfaceMonitor) Close() error { return nil }

func (m *androidInterfaceMonitor) DefaultInterface() *control.Interface {
	return m.defaultIf
}

func (m *androidInterfaceMonitor) OverrideAndroidVPN() bool { return false }
func (m *androidInterfaceMonitor) AndroidVPNEnabled() bool  { return true }

func (m *androidInterfaceMonitor) RegisterCallback(cb tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	return m.callbacks.PushBack(cb)
}

func (m *androidInterfaceMonitor) UnregisterCallback(e *list.Element[tun.DefaultInterfaceUpdateCallback]) {
	m.callbacks.Remove(e)
}

func (m *androidInterfaceMonitor) RegisterMyInterface(name string) {
	m.myInterface = name
}

func (m *androidInterfaceMonitor) MyInterface() string {
	return m.myInterface
}

func (m *androidInterfaceMonitor) detectDefault() {
	if m.netIfName == "" || m.netIfIndex <= 0 {
		log.Println("[autovpn] detectDefault: no interface provided")
		return
	}
	m.defaultIf = &control.Interface{
		Index: m.netIfIndex,
		Name:  m.netIfName,
		MTU:   1500,
	}
	log.Printf("[autovpn] detectDefault: %s (idx=%d)", m.netIfName, m.netIfIndex)
}
