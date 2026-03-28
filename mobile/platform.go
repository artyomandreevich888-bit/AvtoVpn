package mobile

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"
	"unsafe"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	sbLog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
	"github.com/sagernet/sing/service"
	"golang.org/x/sys/unix"

	"github.com/mewmewmemw/autovpn/internal/engine"
)

// androidPlatformProvider implements engine.PlatformProvider for Android.
type androidPlatformProvider struct {
	vpn        VPNService
	tunFd      int
	netIfName  string
	netIfIndex int
}

var _ engine.PlatformProvider = (*androidPlatformProvider)(nil)

func (p *androidPlatformProvider) SetupContext(ctx context.Context) context.Context {
	return service.ContextWith[adapter.PlatformInterface](ctx, &androidPlatform{
		vpn:        p.vpn,
		tunFd:      p.tunFd,
		netIfName:  p.netIfName,
		netIfIndex: p.netIfIndex,
	})
}

func (p *androidPlatformProvider) BoxOptions(opts box.Options) box.Options {
	opts.PlatformLogWriter = &logcatWriter{}
	return opts
}

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
	if p.vpn == nil {
		log.Println("[autovpn] AutoDetectInterfaceControl: vpn is nil!")
		return os.ErrPermission
	}
	ok := p.vpn.Protect(int32(fd))
	if !ok {
		log.Printf("[autovpn] Protect(fd=%d) FAILED", fd)
		return os.ErrPermission
	}
	return nil
}

func (p *androidPlatform) UsePlatformInterface() bool {
	return true
}

// dupFd duplicates a file descriptor. Overridable for testing.
var dupFd = syscall.Dup

// tunNameFromFd reads TUN interface name from fd. Overridable for testing.
var tunNameFromFd = getTunNameSyscall

func (p *androidPlatform) OpenInterface(options *tun.Options, _ option.TunPlatformOptions) (tun.Tun, error) {
	name, err := tunNameFromFd(p.tunFd)
	if err != nil {
		name = "tun0"
	}
	options.Name = name

	if options.InterfaceMonitor != nil {
		options.InterfaceMonitor.RegisterMyInterface(name)
	}

	fd, err := dupFd(p.tunFd)
	if err != nil {
		return nil, fmt.Errorf("dup fd: %w", err)
	}
	options.FileDescriptor = fd
	t, err := tun.New(*options)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func getTunNameSyscall(fd int) (string, error) {
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

// sing-box always calls CreateDefaultInterfaceMonitor when PlatformInterface is set
// (regardless of UsePlatformDefaultInterfaceMonitor return value).
func (p *androidPlatform) UsePlatformDefaultInterfaceMonitor() bool {
	return true
}

func (p *androidPlatform) CreateDefaultInterfaceMonitor(l logger.Logger) tun.DefaultInterfaceMonitor {
	log.Printf("[autovpn] CreateDefaultInterfaceMonitor: name=%q idx=%d", p.netIfName, p.netIfIndex)
	return &androidInterfaceMonitor{
		platform:   p,
		netIfName:  p.netIfName,
		netIfIndex: p.netIfIndex,
	}
}

func (p *androidPlatform) UsePlatformNetworkInterfaces() bool {
	return true
}

func (p *androidPlatform) NetworkInterfaces() ([]adapter.NetworkInterface, error) {
	if p.netIfName == "" || p.netIfIndex == 0 {
		return nil, nil
	}
	ifType := C.InterfaceTypeWIFI
	if p.netIfName != "" && p.netIfName != "wlan0" {
		ifType = C.InterfaceTypeCellular
	}
	return []adapter.NetworkInterface{
		{
			Interface: control.Interface{
				Index: p.netIfIndex,
				Name:  p.netIfName,
				MTU:   1500,
				Flags: net.FlagUp | net.FlagRunning | net.FlagMulticast,
			},
			Type: ifType,
		},
	}, nil
}

// androidInterfaceMonitor provides the default network interface.
type androidInterfaceMonitor struct {
	platform   *androidPlatform
	netIfName  string
	netIfIndex int
	defaultIf  *control.Interface
	myIface    string
	callbacks  list.List[tun.DefaultInterfaceUpdateCallback]
}

func (m *androidInterfaceMonitor) Start() error {
	log.Printf("[autovpn] InterfaceMonitor.Start: name=%q idx=%d", m.netIfName, m.netIfIndex)
	if m.netIfName != "" && m.netIfIndex > 0 {
		m.defaultIf = &control.Interface{
			Index: m.netIfIndex,
			Name:  m.netIfName,
			MTU:   1500,
		}
		log.Printf("[autovpn] InterfaceMonitor: default interface set to %s (idx %d)", m.netIfName, m.netIfIndex)
		// Populate NetworkManager.networkInterfaces — required by selectInterfaces
		// in the dialer. Without this, all connections fail with "no available network interface".
		// Official SFA calls this from libbox/monitor.go; we call it here on startup.
		if m.platform.networkManager != nil {
			if err := m.platform.networkManager.UpdateInterfaces(); err != nil {
				log.Printf("[autovpn] InterfaceMonitor: UpdateInterfaces failed: %v", err)
			}
		}
		// Fire callbacks so NetworkManager logs the default interface.
		for element := m.callbacks.Front(); element != nil; element = element.Next() {
			element.Value(m.defaultIf, 0)
		}
	} else {
		log.Println("[autovpn] InterfaceMonitor: WARNING no default interface — name or index missing")
	}
	return nil
}

func (m *androidInterfaceMonitor) Close() error { return nil }

func (m *androidInterfaceMonitor) DefaultInterface() *control.Interface {
	if m.defaultIf == nil {
		log.Println("[autovpn] DefaultInterface: returning NIL — this will cause 'no available network interface'")
	}
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
	m.myIface = name
}

func (m *androidInterfaceMonitor) MyInterface() string {
	return m.myIface
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

// logcatWriter forwards sing-box logs to Go's log (→ Android logcat).
type logcatWriter struct{}

func (w *logcatWriter) WriteMessage(level sbLog.Level, message string) {
	log.Printf("[sing-box] %v %s", level, message)
}

