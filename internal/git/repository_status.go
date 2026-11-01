package git

import (
	"bytes"
	"fmt"
	"strings"
)

type BranchStatus struct {
	Name, Upstream, Head string
	Ahead, Behind        int
	CountsKnown          bool
	Available            bool
}

func ParseBranchStatus(out []byte) BranchStatus {
	status := BranchStatus{Available: true}
	for _, record := range bytes.Split(out, []byte{0}) {
		line := string(record)
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			status.Head = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			status.Name = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.upstream "):
			status.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			_, err := fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &status.Ahead, &status.Behind)
			status.CountsKnown = err == nil
		}
	}
	return status
}
