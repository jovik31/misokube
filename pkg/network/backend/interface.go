package backend

type backend interface {
	Create(backend_id, subent, host string) error // called to create the entire backend
	Delete() error                                // called to delete the entire backend
	Update() error                                // called to update the backend devices
}

type device interface {
	Get() (string, string, string, error) // get name, ip and mac from device
	Update() error                        // update device fields
	Delete() error                        // delete device from the host
}
