//go:build linux

package network

import (
	"errors"
	"net/netip"
	"reflect"
	"testing"
)

func TestEnsureIPv4Masquerade(t *testing.T) {
	oldNewIPv4IPTables := newIPv4IPTables
	defer func() { newIPv4IPTables = oldNewIPv4IPTables }()

	fake := &fakeIPTablesAppender{}
	newIPv4IPTables = func() (iptablesAppender, error) {
		return fake, nil
	}

	if err := NewLinux().EnsureIPv4Masquerade(
		netip.MustParsePrefix("10.244.0.0/16"),
	); err != nil {
		t.Fatal(err)
	}

	if fake.table != "nat" {
		t.Fatalf("table = %q, want %q", fake.table, "nat")
	}
	if fake.chain != "POSTROUTING" {
		t.Fatalf("chain = %q, want %q", fake.chain, "POSTROUTING")
	}

	want := []string{
		"-s", "10.244.0.0/16",
		"!", "-d", "10.244.0.0/16",
		"-j", "MASQUERADE",
	}
	if !reflect.DeepEqual(fake.rulespec, want) {
		t.Fatalf("rulespec = %v, want %v", fake.rulespec, want)
	}
}

func TestEnsureIPv4MasqueradeMasksPrefix(t *testing.T) {
	oldNewIPv4IPTables := newIPv4IPTables
	defer func() { newIPv4IPTables = oldNewIPv4IPTables }()

	fake := &fakeIPTablesAppender{}
	newIPv4IPTables = func() (iptablesAppender, error) {
		return fake, nil
	}

	if err := NewLinux().EnsureIPv4Masquerade(
		netip.PrefixFrom(netip.MustParseAddr("10.244.4.12"), 16),
	); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"-s", "10.244.0.0/16",
		"!", "-d", "10.244.0.0/16",
		"-j", "MASQUERADE",
	}
	if !reflect.DeepEqual(fake.rulespec, want) {
		t.Fatalf("rulespec = %v, want %v", fake.rulespec, want)
	}
}

func TestEnsureIPv4MasqueradeRejectsInvalidPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix netip.Prefix
	}{
		{
			name:   "invalid",
			prefix: netip.Prefix{},
		},
		{
			name:   "IPv6",
			prefix: netip.MustParsePrefix("fd00::/64"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := NewLinux().EnsureIPv4Masquerade(tt.prefix); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestEnsureIPv4MasqueradeReturnsHandleError(t *testing.T) {
	oldNewIPv4IPTables := newIPv4IPTables
	defer func() { newIPv4IPTables = oldNewIPv4IPTables }()

	want := errors.New("iptables unavailable")
	newIPv4IPTables = func() (iptablesAppender, error) {
		return nil, want
	}

	err := NewLinux().EnsureIPv4Masquerade(
		netip.MustParsePrefix("10.244.0.0/16"),
	)
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want wrapped handle error", err)
	}
}

func TestEnsureIPv4MasqueradeReturnsAppendError(t *testing.T) {
	oldNewIPv4IPTables := newIPv4IPTables
	defer func() { newIPv4IPTables = oldNewIPv4IPTables }()

	want := errors.New("append failed")
	fake := &fakeIPTablesAppender{err: want}
	newIPv4IPTables = func() (iptablesAppender, error) {
		return fake, nil
	}

	err := NewLinux().EnsureIPv4Masquerade(
		netip.MustParsePrefix("10.244.0.0/16"),
	)
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want wrapped append error", err)
	}
}

type fakeIPTablesAppender struct {
	table    string
	chain    string
	rulespec []string
	err      error
}

func (f *fakeIPTablesAppender) AppendUnique(
	table,
	chain string,
	rulespec ...string,
) error {
	f.table = table
	f.chain = chain
	f.rulespec = append([]string(nil), rulespec...)
	return f.err
}
