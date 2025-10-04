package resolver

import "sync/atomic"

type snap struct {
	atomic.Value // *Snapshot
}

func (s *snap) load() Snapshot {

	v := s.Value.Load()
	if v == nil {
		return Snapshot{
			ByUID:  map[string]string{},
			ByName: map[string]string{},
			Synced: false,
		}
	}
	return v.(Snapshot)
}

func (s *snap) store(sn Snapshot) {
	s.Value.Store(sn)
}

func cloneMap(m map[string]string) map[string]string {

	n := make(map[string]string, len(m))
	for k, v := range m {
		n[k] = v
	}
	return n
}
