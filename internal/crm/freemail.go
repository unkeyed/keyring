package crm

import (
	_ "embed"
	"strings"
	"sync"
)

// freemailList is one domain per line. Edit the text file, not Go, to add a
// domain; IsFreeMail loads it into a set on first use.
//
//go:embed freemail.txt
var freemailList string

var freemail = sync.OnceValue(func() map[string]bool {
	set := map[string]bool{}
	for _, line := range strings.Fields(freemailList) {
		set[strings.ToLower(line)] = true
	}
	return set
})
