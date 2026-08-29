// Package dispatch owns everything about reaching a customer's endpoint: the
// HTTP client, the signature, the timeout, and the guard that keeps the worker
// from being turned into a scanner of our own network.
//
// This file holds only the guard. It lands here, and not in the package that
// registers endpoints, because the registration check and the delivery-time
// check must be the same code: two implementations of one rule drift on their
// own, and drifting here means one path allowing what the other blocks.
//
// What this file promises, and what it does not: URLGuard.Validate is defence
// in depth and convenience, NOT the guarantee. DNS can change between
// registration and delivery — a host that resolves to a public address today
// can point at 169.254.169.254 tomorrow, and the worker resolves again when it
// delivers. The load-bearing check is the delivery dialer's, on the address the
// connection is actually about to use, and it calls IsForbiddenAddr — this very
// function. Do not remove that check believing registration already covers it.
// See §4.11 and spec 003.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"time"

	"github.com/victorzix/vhook/internal/errs"
)

// resolveTimeout bounds DNS. A host that takes longer than this is treated as
// unresolvable: registration must not hang on someone else's nameserver.
const resolveTimeout = 3 * time.Second

// Ranges the standard library has no predicate for.
var (
	cgnat         = netip.MustParsePrefix("100.64.0.0/10")
	unspecifiedV4 = netip.MustParsePrefix("0.0.0.0/8")
)

// IsForbiddenAddr reports whether an endpoint must never be allowed to reach
// addr. It is pure and comparable, so the delivery-time dialer can call the
// very same function on the address a connection is about to use.
func IsForbiddenAddr(addr netip.Addr) bool {
	// Unmap first. Without this, ::ffff:10.0.0.1 passes every IPv4 predicate
	// while connecting to 10.0.0.1 — the classic way around a v4-only list.
	addr = addr.Unmap()

	switch {
	case !addr.IsValid():
		return true
	case addr.IsLoopback(): // 127/8, ::1
		return true
	case addr.IsPrivate(): // RFC1918, fc00::/7
		return true
	case addr.IsLinkLocalUnicast(): // 169.254/16, fe80::/10
		return true
	case addr.IsLinkLocalMulticast(), addr.IsInterfaceLocalMulticast(), addr.IsMulticast():
		return true
	case addr.IsUnspecified():
		return true
	case addr.Is4() && cgnat.Contains(addr):
		return true
	case addr.Is4() && unspecifiedV4.Contains(addr):
		return true
	default:
		return false
	}
}

// Resolver is the slice of net.Resolver this package needs. Declared here, by
// the consumer, so tests can answer without touching the network.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// URLGuard validates a destination URL before it is stored.
type URLGuard struct {
	resolver  Resolver
	allowlist map[string]bool
}

// NewURLGuard builds the guard. Hosts in allowlist skip the address check —
// and only that check. The case it exists for is the sink in the compose
// network, which resolves to a private address on purpose.
func NewURLGuard(resolver Resolver, allowlist []string) *URLGuard {
	allowed := make(map[string]bool, len(allowlist))
	for _, host := range allowlist {
		if host != "" {
			allowed[host] = true
		}
	}
	return &URLGuard{resolver: resolver, allowlist: allowed}
}

// Validate checks scheme, resolves the host, and rejects if any resolved
// address is forbidden.
//
// This is defence in depth, NOT the guarantee. DNS can change between
// registration and delivery, so the load-bearing check is the dialer's, on the
// address the connection actually uses. See the package comment and spec 003.
func (g *URLGuard) Validate(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.Join(errs.InvalidEndpointURL, fmt.Errorf("dispatch: parse url: %w", err))
	}
	if parsed.Scheme != "https" {
		return errors.Join(errs.InvalidEndpointURL,
			errors.New("dispatch: endpoint url must use https"))
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.Join(errs.InvalidEndpointURL, errors.New("dispatch: url has no host"))
	}

	// Exact match only. Suffix matching would let "evil-sink" through on an
	// allowlist meant for "sink".
	if g.allowlist[host] {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	addrs, err := g.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return errors.Join(errs.UnresolvableHost, fmt.Errorf("dispatch: resolve %q: %w", host, err))
	}
	if len(addrs) == 0 {
		return errors.Join(errs.UnresolvableHost,
			fmt.Errorf("dispatch: %q resolved to no addresses", host))
	}

	// One public address among several does not save it: any forbidden
	// address means the destination can be reached inside our network.
	for _, addr := range addrs {
		if IsForbiddenAddr(addr) {
			return errors.Join(errs.ForbiddenAddress,
				fmt.Errorf("dispatch: %q resolves to a forbidden address", host))
		}
	}
	return nil
}
