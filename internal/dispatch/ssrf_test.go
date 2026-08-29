package dispatch_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/victorzix/vhook/internal/dispatch"
	"github.com/victorzix/vhook/internal/errs"
)

func TestIsForbiddenAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
		why  string
	}{
		{"10.0.0.1", true, "RFC1918"},
		{"172.16.0.1", true, "RFC1918"},
		{"172.31.255.255", true, "RFC1918"},
		{"192.168.1.1", true, "RFC1918"},
		{"127.0.0.1", true, "loopback"},
		{"169.254.169.254", true, "link-local: metadados de cloud"},
		{"100.64.0.1", true, "CGNAT"},
		{"0.0.0.0", true, "não roteável"},
		{"::1", true, "loopback IPv6"},
		{"fc00::1", true, "ULA IPv6"},
		{"fe80::1", true, "link-local IPv6"},
		// O driblador clássico: lista que só olha IPv4 deixa passar isto,
		// e a conexão acaba em 10.0.0.1.
		{"::ffff:10.0.0.1", true, "IPv4 privado mapeado em IPv6"},
		{"::ffff:169.254.169.254", true, "link-local mapeado em IPv6"},
		// Estes dois são os que realmente dependem do Unmap() da nossa
		// implementação. Os predicados do net/netip — IsPrivate, IsLoopback,
		// IsLinkLocalUnicast — já desmapeiam 4-em-6 por dentro, então os dois
		// casos acima passariam mesmo sem a linha. Já IsUnspecified não
		// desmapeia, e os prefixos de CGNAT e 0.0.0.0/8 estão atrás de uma
		// guarda Is4(), falsa para 4-em-6. Sem estes casos, apagar o Unmap()
		// num refactor não produziria nenhum teste vermelho.
		{"::ffff:100.64.0.1", true, "CGNAT mapeado: a guarda Is4() falha sem Unmap"},
		{"::ffff:0.0.0.0", true, "não roteável mapeado: IsUnspecified não desmapeia"},

		{"1.1.1.1", false, "público"},
		{"172.32.0.1", false, "fora do bloco RFC1918"},
		{"100.128.0.1", false, "fora do CGNAT"},
		{"2606:4700::1111", false, "público IPv6"},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			addr, err := netip.ParseAddr(tt.addr)
			if err != nil {
				t.Fatalf("ParseAddr: %v", err)
			}
			if got := dispatch.IsForbiddenAddr(addr); got != tt.want {
				t.Errorf("IsForbiddenAddr(%s) = %v, want %v — %s",
					tt.addr, got, tt.want, tt.why)
			}
		})
	}
}

// fakeResolver devolve o que o teste mandar, sem tocar a rede.
type fakeResolver struct {
	byHost map[string][]string
	err    error
}

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if f.err != nil {
		return nil, f.err
	}
	raw, ok := f.byHost[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	out := make([]netip.Addr, 0, len(raw))
	for _, s := range raw {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

func guard(t *testing.T, hosts map[string][]string, allowlist ...string) *dispatch.URLGuard {
	t.Helper()
	return dispatch.NewURLGuard(fakeResolver{byHost: hosts}, allowlist)
}

func TestValidateAcceptsAPublicHTTPSURL(t *testing.T) {
	g := guard(t, map[string][]string{"api.cliente.com": {"1.1.1.1"}})
	if err := g.Validate(context.Background(), "https://api.cliente.com/hooks"); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestValidateRejectsNonHTTPS(t *testing.T) {
	g := guard(t, map[string][]string{"api.cliente.com": {"1.1.1.1"}})
	for _, raw := range []string{
		"http://api.cliente.com/hooks",
		"ftp://api.cliente.com/hooks",
		"api.cliente.com/hooks",
		"",
		"https://",
		"não é uma url",
	} {
		t.Run(raw, func(t *testing.T) {
			if err := g.Validate(context.Background(), raw); !errors.Is(err, errs.InvalidEndpointURL) {
				t.Errorf("error = %v, queria errs.InvalidEndpointURL", err)
			}
		})
	}
}

func TestValidateRejectsAForbiddenAddress(t *testing.T) {
	g := guard(t, map[string][]string{"interno.exemplo.com": {"10.0.0.1"}})
	err := g.Validate(context.Background(), "https://interno.exemplo.com/hooks")
	if !errors.Is(err, errs.ForbiddenAddress) {
		t.Errorf("error = %v, queria errs.ForbiddenAddress", err)
	}
}

// Um IP público entre vários não salva: basta um proibido para recusar.
func TestValidateRejectsWhenAnyResolvedAddressIsForbidden(t *testing.T) {
	g := guard(t, map[string][]string{"misto.exemplo.com": {"1.1.1.1", "10.0.0.1"}})
	err := g.Validate(context.Background(), "https://misto.exemplo.com/hooks")
	if !errors.Is(err, errs.ForbiddenAddress) {
		t.Errorf("error = %v, queria errs.ForbiddenAddress", err)
	}
}

func TestValidateRejectsAnUnresolvableHost(t *testing.T) {
	g := guard(t, map[string][]string{})
	err := g.Validate(context.Background(), "https://naoexiste.exemplo.com/hooks")
	if !errors.Is(err, errs.UnresolvableHost) {
		t.Errorf("error = %v, queria errs.UnresolvableHost", err)
	}
}

func TestValidateRejectsAHostThatResolvesToNothing(t *testing.T) {
	g := guard(t, map[string][]string{"vazio.exemplo.com": {}})
	err := g.Validate(context.Background(), "https://vazio.exemplo.com/hooks")
	if !errors.Is(err, errs.UnresolvableHost) {
		t.Errorf("error = %v, queria errs.UnresolvableHost", err)
	}
}

// O sink roda no compose e resolve para um IP privado. A allowlist existe
// exatamente para ele.
func TestAllowlistedHostSkipsTheAddressCheck(t *testing.T) {
	g := guard(t, map[string][]string{"sink": {"172.18.0.5"}}, "sink")
	if err := g.Validate(context.Background(), "https://sink/hooks"); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

// Mas a allowlist não perdoa http: ela dispensa a checagem de faixa, não o TLS.
func TestAllowlistedHostStillRequiresHTTPS(t *testing.T) {
	g := guard(t, map[string][]string{"sink": {"172.18.0.5"}}, "sink")
	if err := g.Validate(context.Background(), "http://sink/hooks"); !errors.Is(err, errs.InvalidEndpointURL) {
		t.Errorf("error = %v, queria errs.InvalidEndpointURL", err)
	}
}

func TestAllowlistDoesNotMatchBySuffix(t *testing.T) {
	// "sink" na allowlist não pode liberar "evil-sink" nem "sink.evil.com".
	g := guard(t, map[string][]string{
		"evil-sink":     {"10.0.0.1"},
		"sink.evil.com": {"10.0.0.1"},
	}, "sink")
	for _, host := range []string{"evil-sink", "sink.evil.com"} {
		t.Run(host, func(t *testing.T) {
			err := g.Validate(context.Background(), "https://"+host+"/hooks")
			if !errors.Is(err, errs.ForbiddenAddress) {
				t.Errorf("error = %v, queria errs.ForbiddenAddress", err)
			}
		})
	}
}

// Um IP literal na URL não pode pular a checagem por não ter o que resolver.
func TestValidateRejectsAForbiddenIPLiteral(t *testing.T) {
	g := guard(t, map[string][]string{"169.254.169.254": {"169.254.169.254"}})
	err := g.Validate(context.Background(), "https://169.254.169.254/latest/meta-data/")
	if !errors.Is(err, errs.ForbiddenAddress) {
		t.Errorf("error = %v, queria errs.ForbiddenAddress", err)
	}
}
