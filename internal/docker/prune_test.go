package docker

import (
	"reflect"
	"testing"
)

func TestPruneSpecFor(t *testing.T) {
	tests := []struct {
		operation string
		auditName string
		args      []string
	}{
		{OperationImages, "prune_images", []string{"image", "prune", "-a", "-f"}},
		{OperationAll, "prune_all", []string{"system", "prune", "-a", "--volumes", "-f"}},
	}
	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			spec, ok := PruneSpecFor(tt.operation)
			if !ok || spec.AuditOperation != tt.auditName || !reflect.DeepEqual(spec.Args, tt.args) {
				t.Fatalf("PruneSpecFor(%q) = (%+v, %v), want audit %q args %v", tt.operation, spec, ok, tt.auditName, tt.args)
			}
		})
	}
	if _, ok := PruneSpecFor("invalid"); ok {
		t.Fatal("invalid operation unexpectedly returned a prune command")
	}
}
