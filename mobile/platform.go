package mobile

import (
	"context"
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	sbLog "github.com/sagernet/sing-box/log"
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
	if p.vpn.Protect(int32(fd)) {
		return nil
	}
	return os.ErrPermission
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
	return &androidInterfaceMonitor{
		platform:   p,
		netIfName:  p.netIfName,
		netIfIndex: p.netIfIndex,
	}
}

func (p *androidPlatform) UsePlatformNetworkInterfaces() bool {
	return false
}

func (p *androidPlatform) NetworkInterfaces() ([]adapter.NetworkInterface, error) {
	return nil, nil
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
	if m.netIfName != "" && m.netIfIndex > 0 {
		m.defaultIf = &control.Interface{
			Index: m.netIfIndex,
			Name:  m.netIfName,
			MTU:   1500,
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

