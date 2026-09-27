package xray

import (
	"context"
	"testing"
	"time"

	"github.com/cedar2025/xboard-node/internal/model"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
)

var udpDest = net.UDPDestination(net.ParseAddress("9.9.9.9"), 53)

func userContextFrom(email, ip string) context.Context {
	return session.ContextWithInbound(context.Background(), &session.Inbound{
		Source: net.TCPDestination(net.ParseAddress(ip), 40000),
		User:   &protocol.MemoryUser{Email: email},
	})
}

func limitedDispatcher(limit int) (*LimitDispatcher, string) {
	ld, _ := newRevocationDispatcher()
	email := userEmail(1)
	ld.UpdateLimits(map[string]int{email: 1}, map[string]int{email: limit}, nil)
	return ld, email
}

// The panel pushes the IPs a user has online on every node; the device limit
// is per user, not per node.
func TestLimitDispatcher_GlobalDevicesCountTowardLimit(t *testing.T) {
	ld, email := limitedDispatcher(2)
	ld.UpdateGlobalDevices(map[int][]string{1: {"5.5.5.5", "6.6.6.6"}})

	if err := ld.DispatchLink(userContextFrom(email, "7.7.7.7"), tcpDest, newTestLink().link); err == nil {
		t.Error("a third device was admitted while two are online on other nodes")
	}
	if err := ld.DispatchLink(userContextFrom(email, "5.5.5.5"), tcpDest, newTestLink().link); err != nil {
		t.Errorf("a device already online on another node was rejected: %v", err)
	}
}

func TestLimitDispatcher_IgnoresStaleOrClearedGlobalDevices(t *testing.T) {
	ld, email := limitedDispatcher(1)
	ld.UpdateGlobalDevices(map[int][]string{1: {"5.5.5.5"}})
	ld.globalLastUpdate = time.Now().Add(-2 * globalDevicesTTL)
	if err := ld.DispatchLink(userContextFrom(email, "7.7.7.7"), tcpDest, newTestLink().link); err != nil {
		t.Errorf("stale global devices still counted: %v", err)
	}

	ld, email = limitedDispatcher(1)
	ld.UpdateGlobalDevices(map[int][]string{1: {"5.5.5.5"}})
	ld.ClearGlobalDevices()
	if err := ld.DispatchLink(userContextFrom(email, "7.7.7.7"), tcpDest, newTestLink().link); err != nil {
		t.Errorf("cleared global devices still counted: %v", err)
	}
}

// A device that only sends UDP (XUDP, Shadowsocks UDP) is still a device.
func TestLimitDispatcher_UDPDevicesCountTowardLimit(t *testing.T) {
	ld, email := limitedDispatcher(1)
	udp := newTestLink()
	if err := ld.DispatchLink(userContextFrom(email, "1.1.1.1"), udpDest, udp.link); err != nil {
		t.Fatalf("UDP device rejected: %v", err)
	}

	if err := ld.DispatchLink(userContextFrom(email, "2.2.2.2"), tcpDest, newTestLink().link); err == nil {
		t.Error("a second device was admitted while a UDP-only device holds the only slot")
	}
	if alive, _ := ld.GetConnectionState(); !alive[1]["1.1.1.1"] {
		t.Error("UDP-only device is not reported online")
	}

	udp.link.Writer.(interface{ Close() error }).Close()
	if err := ld.DispatchLink(userContextFrom(email, "2.2.2.2"), tcpDest, newTestLink().link); err != nil {
		t.Errorf("slot not released after the UDP device disconnected: %v", err)
	}
}

func TestXrayGlobalDevicesLimitNewDevices(t *testing.T) {
	user := model.UserSpec{ID: 1, UUID: rotationOldUUID, DeviceLimit: 1}
	target := rotationEchoServer(t)
	x, addr := newRotationXray(t, []model.UserSpec{user})

	// The only device slot is taken on another node by an IP that sorts
	// before this client's 127.0.0.1.
	x.UpdateGlobalDevices(map[int][]string{1: {"1.1.1.1"}})
	if c, err := openRotationEcho(addr, user.UUID, target); err == nil {
		c.Close()
		t.Fatal("connection admitted although the user's device limit is used on another node")
	}

	x.ClearGlobalDevices()
	requireRotationEcho(t, addr, user.UUID, target).Close()
}
