package docker

const (
	OperationImages = "images"
	OperationAll    = "all"
)

type PruneSpec struct {
	AuditOperation string
	Args           []string
}

// PruneSpecFor returns the Docker command shared by cleanup previews and execution.
func PruneSpecFor(operation string) (PruneSpec, bool) {
	switch operation {
	case OperationImages:
		return PruneSpec{AuditOperation: "prune_images", Args: []string{"image", "prune", "-a", "-f"}}, true
	case OperationAll:
		return PruneSpec{AuditOperation: "prune_all", Args: []string{"system", "prune", "-a", "--volumes", "-f"}}, true
	default:
		return PruneSpec{}, false
	}
}
