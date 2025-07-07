package persist

type JSON struct {
	Define      string
	Alternative string
	Status      int
}

// Recover all the different aspects of the IP dynamic allocation
func DefineAlloc() string {

	return "Total amount of allocations, precise memory statements"
}

func Alternative() int {

	return 1
}
