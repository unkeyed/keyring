package crm

import _ "embed"

// freemailList is one domain per line. Edit the text file, not Go, to add a
// domain; IsFreeMail loads it into a set on first use.
//
//go:embed freemail.txt
var freemailList string
