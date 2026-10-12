package xray

import (
	"context"
	"testing"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/model"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
)

// spliceCandidateContext is a session whose inbound may still switch to
// splice copy, the state a VLESS Vision inbound dispatches with.
func spliceCandidateContext(email string) (context.Context, *session.Inbound) {
	inbound := &session.Inbound{
		Source:        net.TCPDestination(net.ParseAddress("1.1.1.1"), 40000),
		User:          &protocol.MemoryUser{Email: email},
		CanSpliceCopy: 2,
	}
	return session.ContextWithInbound(context.Background(), inbound), inbound
}

func TestLimitDispatcher_DispatchLinkDisablesSpliceForSpeedLimitedUser(t *testing.T) {
	ld, _ := newRevocationDispatcher()
	email := userEmail(1)
	ld.UpdateLimits(map[string]int{email: 1}, nil, map[string]int{email: 10})

	ctx, inbound := spliceCandidateContext(email)
	if err := ld.DispatchLink(ctx, tcpDest, newTestLink().link); err != nil {
		t.Fatalf("DispatchLink() error = %v", err)
	}

	if inbound.CanSpliceCopy != 3 {
		t.Fatalf("CanSpliceCopy = %d, want 3: splice copy bypasses the rate limiter and the user traffic counter", inbound.CanSpliceCopy)
	}
}

func TestLimitDispatcher_DispatchLinkKeepsSpliceForUnlimitedUser(t *testing.T) {
	ld, _ := newRevocationDispatcher()
	limited, unlimited := userEmail(1), userEmail(2)
	ld.UpdateLimits(map[string]int{limited: 1, unlimited: 2}, nil, map[string]int{limited: 10})

	ctx, inbound := spliceCandidateContext(unlimited)
	if err := ld.DispatchLink(ctx, tcpDest, newTestLink().link); err != nil {
		t.Fatalf("DispatchLink() error = %v", err)
	}

	if inbound.CanSpliceCopy != 2 {
		t.Fatalf("CanSpliceCopy = %d, want 2: users without a speed limit keep splice copy", inbound.CanSpliceCopy)
	}
}

func TestXrayUpdateDispatcherLimitsDisablesSpliceForSpeedLimitedUsers(t *testing.T) {
	ld, _ := newRevocationDispatcher()
	x := New(config.KernelConfig{Type: "xray"})
	x.limitDispatcher = ld

	x.updateDispatcherLimits([]model.UserSpec{
		{ID: 1, UUID: "uuid-1", SpeedLimit: 10},
		{ID: 2, UUID: "uuid-2"},
	})

	for _, tc := range []struct {
		userID int
		want   int
	}{
		{userID: 1, want: 3},
		{userID: 2, want: 2},
	} {
		ctx, inbound := spliceCandidateContext(userEmail(tc.userID))
		if err := ld.DispatchLink(ctx, tcpDest, newTestLink().link); err != nil {
			t.Fatalf("DispatchLink(user %d) error = %v", tc.userID, err)
		}
		if inbound.CanSpliceCopy != tc.want {
			t.Errorf("user %d: CanSpliceCopy = %d, want %d", tc.userID, inbound.CanSpliceCopy, tc.want)
		}
	}
}
