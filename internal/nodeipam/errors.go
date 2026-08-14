package nodeipam

import "errors"

var (
	// ErrNotRestored reports that the manager is not ready to change state.
	ErrNotRestored = errors.New("nodeipam: state not restored")

	// ErrInvalidOwner reports an allocation owner with missing identity fields.
	ErrInvalidOwner = errors.New("nodeipam: invalid owner")

	// ErrInvalidRequest reports an allocation request with missing metadata.
	ErrInvalidRequest = errors.New("nodeipam: invalid allocation request")

	// ErrOwnerConflict reports a repeated owner with different pod metadata.
	ErrOwnerConflict = errors.New("nodeipam: owner conflict")

	// ErrCorruptState reports persisted allocation state that is not valid.
	ErrCorruptState = errors.New("nodeipam: corrupt state")

	// ErrInvalidDependency reports a missing allocator or store dependency.
	ErrInvalidDependency = errors.New("nodeipam: invalid dependency")
)
