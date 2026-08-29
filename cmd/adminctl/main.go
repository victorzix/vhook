// Command adminctl carries the operational tasks that have no place in an HTTP
// surface: creating the first tenant, and minting the master key it needs.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/victorzix/vhook/internal/errs"
)

const usage = `adminctl — operational commands for vhook

  genkey      print a fresh VHOOK_MASTER_KEY
  bootstrap   create the first organization and application
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		// The code is what the operator reports; the wrapped detail is what
		// they read. Neither ever carries the master key or the api key.
		var registered *errs.Error
		if errors.As(err, &registered) {
			fmt.Fprintf(os.Stderr, "error: %s\n%v\n", registered.Code, err)
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(out, usage)
		return errors.New("adminctl: no subcommand given")
	}

	switch args[0] {
	case "genkey":
		return genkey(out)
	case "bootstrap":
		return bootstrap(args[1:], out)
	default:
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("adminctl: unknown subcommand %q", args[0])
	}
}
