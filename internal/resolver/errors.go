package resolver

import "fmt"

type Error string

func (e Error) Error() string { return string(e) }

var (
	ErrNotReady            = Error("resolver not ready")
	ErrNotIndexed          = Error("pod not indexed")
	ErrNoTenantLabel       = Error("pod missing tenant label")
	ErrEmptyTenantLabelKey = Error("tenant label key cannot be empty")
	ErrUnknownTenant       = Error("tenant unknown")
	ErrEmptyNodeName       = Error("node name cannot be empty")
)

type UnknownTenantError struct {
	Namespace string
	Pod       string
}

func (e UnknownTenantError) Error() string {
	return fmt.Sprintf("%v for %s/%s", ErrUnknownTenant, e.Namespace, e.Pod)
}

func (e UnknownTenantError) Unwrap() error { return ErrUnknownTenant }

type NotIndexedError struct {
	Namespace, Pod, UID string
}

func (e NotIndexedError) Error() string {
	if e.UID != "" {
		return fmt.Sprintf("%v: %s/%s (uid=%s)", ErrNotIndexed, e.Namespace, e.Pod, e.UID)
	}
	return fmt.Sprintf("%v: %s/%s", ErrNotIndexed, e.Namespace, e.Pod)
}
func (e NotIndexedError) Unwrap() error { return ErrNotIndexed }

type NoTenantLabelError struct {
	Namespace, Pod, UID string
}

func (e NoTenantLabelError) Error() string {
	if e.UID != "" {
		return fmt.Sprintf("%v: %s/%s (uid=%s)", ErrNoTenantLabel, e.Namespace, e.Pod, e.UID)
	}
	return fmt.Sprintf("%v: %s/%s", ErrNoTenantLabel, e.Namespace, e.Pod)
}
func (e NoTenantLabelError) Unwrap() error { return ErrNoTenantLabel }
