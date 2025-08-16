package fdb

import (
	"reflect"
	"testing"
)

func TestRegisterNil(t *testing.T) {

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when registering nil, got none")
		}
	}()
	RegisterFDBManager(nil)

}

func TestRegisterFDBManager(t *testing.T) {

	mock := &mockMgr{}
	RegisterFDBManager(mock)
	mgr := Manager()
	if mgr != mock {
		t.Fatalf("Manager() = %v; want %v", mgr, mock)
	}

	entry := FDBEntry{Device: "vx1"}
	if err := mgr.Add(entry); err != nil {
		t.Fatalf("Add() error %v", err)
	}

	if !reflect.DeepEqual(mock.adds, []FDBEntry{entry}) {
		t.Errorf("adds = %v; want %v", mock.adds, entry)
	}

	if err := mgr.Update(entry); err != nil {
		t.Fatalf("Update() error %v", err)
	}
	if !reflect.DeepEqual(mock.updates, []FDBEntry{entry}) {
		t.Errorf("updates = %v; want %v", mock.updates, entry)
	}

	if err := mgr.Delete(entry); err != nil {
		t.Fatalf("Delete() error %v", err)
	}

	if !reflect.DeepEqual(mock.dels, []FDBEntry{entry}) {
		t.Errorf("dels = %v; want %v", mock.dels, entry)
	}

}
