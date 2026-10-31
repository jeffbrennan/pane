package main

import (
	"bytes"
	"fmt"
	"strings"
)

type branchStatus struct {
	name, upstream, head string
	ahead, behind        int
	countsKnown          bool
	available            bool
}

func parseBranchStatus(out []byte) branchStatus {
	status := branchStatus{available: true}
	for _, record := range bytes.Split(out, []byte{0}) {
		line := string(record)
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			status.head = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			status.name = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.upstream "):
			status.upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			_, err := fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &status.ahead, &status.behind)
			status.countsKnown = err == nil
		}
	}
	return status
}
