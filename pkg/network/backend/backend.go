package backend

var _ backend = (*Backend_Manager)(nil)

type Backend_Manager struct {
	device_map map[string]*Device // device name --> device info
}

func (bm *Backend_Manager) Create() (error, []*Device) {

	return nil, nil
}
func (bm *Backend_Manager) Update(device *Device, val any) error { return nil }
func (bm *Backend_Manager) Delete([]*Device) error               { return nil }
