package engine

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/wang4386/CDT-Monitor/internal/aliyun"
	"github.com/wang4386/CDT-Monitor/internal/domain"
	"github.com/wang4386/CDT-Monitor/internal/notify"
	"github.com/wang4386/CDT-Monitor/internal/store"
)

type ipv6TestProvider struct {
	billingTestProvider
	details aliyun.InstanceDetails
}

func (p *ipv6TestProvider) GetInstanceDetails(context.Context, domain.Account, string) (aliyun.InstanceDetails, error) {
	return p.details, nil
}

func TestIPv6LookupFailureKeepsAddressButUpdatesStatus(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	config := domain.Config{
		AdminPassword: "Strong-Password-42!", TrafficThreshold: 95,
		ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify",
		APIInterval: 600, Timezone: "Asia/Shanghai",
		Accounts: []domain.Account{{AccessKeyID: "LTAItest", AccessKeySecret: "secret", RegionID: "cn-hongkong", InstanceID: "i-test", MaxTraffic: 200, SiteType: "china"}},
	}
	if err := st.Setup(ctx, config); err != nil {
		t.Fatal(err)
	}
	accounts, err := st.ListAccounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts=%v err=%v", accounts, err)
	}
	id := accounts[0].ID
	if err := st.UpdateRuntimeWithIPs(ctx, id, 0, domain.StatusStopped, "203.0.113.1", "2001:db8::1", time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	p := &ipv6TestProvider{details: aliyun.InstanceDetails{Status: domain.StatusRunning, PublicIP: "203.0.113.2", IPv6Error: errors.New("permission denied")}}
	e := New(st, p, notify.New(), slog.Default(), 1)
	if _, err := e.processAccount(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	a, err := st.GetAccount(ctx, id)
	if err != nil || a.InstanceStatus != domain.StatusRunning || a.PublicIP != "203.0.113.2" || a.PublicIPv6 != "2001:db8::1" || a.TrafficUsed != 1.25 {
		t.Fatalf("account=%#v err=%v", a, err)
	}
	logs, err := st.ListLogs(ctx, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range logs {
		if entry.Type == "warning" && strings.Contains(entry.Message, "ecs:DescribeNetworkInterfaces") && strings.Contains(entry.Message, "permission denied") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing actionable warning: %#v", logs)
	}
	// A later successful lookup must update the address, or clear a removed address.
	for _, ip := range []string{"2001:db8::2", ""} {
		p.details.IPv6Error, p.details.PublicIPv6 = nil, ip
		if _, err := e.processAccount(ctx, id, true); err != nil {
			t.Fatal(err)
		}
		a, err := st.GetAccount(ctx, id)
		if err != nil || a.PublicIPv6 != ip {
			t.Fatalf("account=%#v err=%v want IPv6=%q", a, err, ip)
		}
	}
}
