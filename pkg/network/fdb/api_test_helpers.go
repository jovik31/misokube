package fdb

type mockMgr struct {
	adds    []FDBEntry
	updates []FDBEntry
	dels    []FDBEntry
}

func (mgr *mockMgr) Add(e FDBEntry) error {

	mgr.adds = append(mgr.adds, e)
	return nil
}

func (mgr *mockMgr) Update(e FDBEntry) error {

	mgr.updates = append(mgr.updates, e)
	return nil
}

func (mgr *mockMgr) Delete(e FDBEntry) error {

	mgr.dels = append(mgr.dels, e)
	return nil
}
