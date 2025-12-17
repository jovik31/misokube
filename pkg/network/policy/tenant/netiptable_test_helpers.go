package tenant

import (
	"fmt"
)

// Fake Handle for unit tests (records calls/args; no root needed)
type fakeIPT struct {
	Calls []string
	Args  [][]string
	Err   error
}

func (f *fakeIPT) NewChain(t, c string) error {
	f.Calls = append(f.Calls, "NewChain")
	f.Args = append(f.Args, []string{t, c})
	return f.Err
}
func (f *fakeIPT) ClearChain(t, c string) error {
	f.Calls = append(f.Calls, "ClearChain")
	f.Args = append(f.Args, []string{t, c})
	return f.Err
}
func (f *fakeIPT) DeleteChain(t, c string) error {
	f.Calls = append(f.Calls, "DeleteChain")
	f.Args = append(f.Args, []string{t, c})
	return f.Err
}
func (f *fakeIPT) AppendUnique(t, c string, rs ...string) error {
	f.Calls = append(f.Calls, "AppendUnique")
	f.Args = append(f.Args, append([]string{t, c}, rs...))
	return f.Err
}
func (f *fakeIPT) InsertUnique(t, c string, pos int, rs ...string) error {
	f.Calls = append(f.Calls, "InsertUnique")
	posS := fmt.Sprintf("pos=%d", pos)
	f.Args = append(f.Args, append([]string{t, c, posS}, rs...))
	return f.Err
}
func (f *fakeIPT) Delete(t, c string, rs ...string) error {
	f.Calls = append(f.Calls, "Delete")
	f.Args = append(f.Args, append([]string{t, c}, rs...))
	return f.Err
}
