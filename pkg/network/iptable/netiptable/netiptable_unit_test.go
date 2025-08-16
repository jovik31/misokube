package netiptable

import (
	"reflect"
	"testing"
)

func TestAddClusterMasquerade(t *testing.T) {
	f := &fakeIPT{}
	m := NewIPtableManager(f)

	if err := m.AddClusterMasquerade("10.244.0.0/16"); err != nil {
		t.Fatal(err)
	}
	want := []string{"nat", "POSTROUTING", "-s", "10.244.0.0/16", "!", "-d", "10.244.0.0/16", "-j", "MASQUERADE"}
	if got := f.Args[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("AppendUnique args = %v, want %v", got, want)
	}
}

func TestEnsureTenantChains(t *testing.T) {
	f := &fakeIPT{}
	m := NewIPtableManager(f)

	if err := m.EnsureTenantChains("TEN"); err != nil {
		t.Fatal(err)
	}
	// Expect: NewChain/ ClearChain filter FW-TEN
	if len(f.Calls) != 2 ||
		f.Calls[0] != "NewChain" || f.Args[0][0] != "filter" || f.Args[0][1] != "FW-TEN" ||
		f.Calls[1] != "ClearChain" || f.Args[1][0] != "filter" || f.Args[1][1] != "FW-TEN" {
		t.Fatalf("calls=%v args=%v", f.Calls, f.Args)
	}
}

func TestDeleteTenantChains(t *testing.T) {
	f := &fakeIPT{}
	m := NewIPtableManager(f)

	if err := m.DeleteTenantChains("TEN"); err != nil {
		t.Fatal(err)
	}
	// Expect: Clear+Delete filter FW-TEN
	if len(f.Calls) != 2 ||
		f.Calls[0] != "ClearChain" || f.Args[0][0] != "filter" || f.Args[0][1] != "FW-TEN" ||
		f.Calls[1] != "DeleteChain" || f.Args[1][0] != "filter" || f.Args[1][1] != "FW-TEN" {
		t.Fatalf("calls=%v args=%v", f.Calls, f.Args)
	}
}

func TestEnsureTenantIsolationByIface(t *testing.T) {
	f := &fakeIPT{}
	m := NewIPtableManager(f)

	err := m.EnsureTenantIsolationByIface("A", "br-A", "vx-A", "eth0")
	if err != nil {
		t.Fatal(err)
	}

	// We expect (order among AppendUnique of same chain is preserved by our calls):
	want := [][]string{
		{"filter", "FORWARD", "-i", "br-A", "-j", "FW-A"},
		{"filter", "FORWARD", "-i", "vx-A", "-j", "FW-A"},
		{"filter", "FW-A", "-o", "br-A", "-j", "ACCEPT"},
		{"filter", "FW-A", "-o", "vx-A", "-j", "ACCEPT"},
		{"filter", "FW-A", "-o", "eth0", "-j", "ACCEPT"},
		{"filter", "FW-A", "-m", "comment", "--comment", "tenant:A:default-drop", "-j", "DROP"},
	}
	var got [][]string
	for i, c := range f.Calls {
		if c == "AppendUnique" {
			got = append(got, f.Args[i])
		}
	}
	if len(got) < len(want) {
		t.Fatalf("got %d rules, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("step %d args=%v want=%v", i, got[i], want[i])
		}
	}
}

func TestEnsureDefaultTenantPassByIface_Order(t *testing.T) {
	f := &fakeIPT{}
	m := NewIPtableManager(f)

	err := m.EnsureDefaultTenantPassByIface("br-def", "vx-def")
	if err != nil {
		t.Fatal(err)
	}

	// Inserted in reverse with pos=1, so recorded InsertUnique calls should be:
	want := [][]string{
		{"filter", "FORWARD", "pos=1", "-o", "vx-def", "-j", "ACCEPT"},
		{"filter", "FORWARD", "pos=1", "-o", "br-def", "-j", "ACCEPT"},
		{"filter", "FORWARD", "pos=1", "-i", "vx-def", "-j", "ACCEPT"},
		{"filter", "FORWARD", "pos=1", "-i", "br-def", "-j", "ACCEPT"},
	}
	var got [][]string
	for i, c := range f.Calls {
		if c == "InsertUnique" {
			got = append(got, f.Args[i])
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("insert sequence mismatch:\n got=%v\nwant=%v", got, want)
	}
}

/*func TestEnsureForwardFastPath(t *testing.T) {
	f := &fakeIPT{}
	m := NewIPtableManager(f)

	if err := m.EnsureForwardFastPath(); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 || f.Calls[0] != "InsertUnique" {
		t.Fatalf("expected InsertUnique once, got %v", f.Calls)
	}
	want := []string{"filter", "FORWARD", "pos=1", "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"}
	if got := f.Args[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("fast-path args=%v want=%v", got, want)
	}
}

func TestDeleteRule(t *testing.T) {
	f := &fakeIPT{}
	m := NewIPtableManager(f)

	if err := m.DeleteRule("filter", "FORWARD", "-i", "br-X", "-j", "FW-X"); err != nil {
		t.Fatal(err)
	}
	want := []string{"filter", "FORWARD", "-i", "br-X", "-j", "FW-X"}
	if got := f.Args[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("delete args=%v want=%v", got, want)
	}
}*/
