package mail

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Resolver is the subset of DNS lookups the checklist needs (swappable in tests).
type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupHost(ctx context.Context, name string) ([]string, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

type Record struct {
	Purpose  string `json:"purpose"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Where    string `json:"where"`    // dns | host provider
	Required bool   `json:"required"` // mail will not work (or be delivered) without it
	Status   string `json:"status"`   // ok | missing | wrong
	Found    string `json:"found,omitempty"`
}

// Checklist lists the DNS records the Administrator must create for a Mail Domain.
func (m *Manager) Checklist(domain string) ([]Record, error) {
	var sel string
	if err := m.db.QueryRow(`SELECT selector FROM domains WHERE name=?`, domain).Scan(&sel); err != nil {
		return nil, ErrNotFound
	}
	pub, err := m.dkimPublic(domain, sel)
	if err != nil {
		return nil, err
	}
	h := m.cfg.Hostname
	recs := []Record{
		{Purpose: "Incoming mail", Type: "MX", Name: domain, Value: "10 " + h + ".", Where: "dns", Required: true},
	}
	if m.cfg.ServerIPv4 != "" {
		recs = append(recs, Record{Purpose: "Mail server address", Type: "A", Name: h, Value: m.cfg.ServerIPv4, Where: "dns", Required: true})
	}
	if m.cfg.ServerIPv6 != "" {
		recs = append(recs, Record{Purpose: "Mail server address (IPv6)", Type: "AAAA", Name: h, Value: m.cfg.ServerIPv6, Where: "dns"})
	}
	recs = append(recs,
		Record{Purpose: "SPF: who may send for this domain", Type: "TXT", Name: domain, Value: "v=spf1 mx -all", Where: "dns", Required: true},
		Record{Purpose: "DKIM: signature key", Type: "TXT", Name: sel + "._domainkey." + domain, Value: "v=DKIM1; k=rsa; p=" + pub, Where: "dns", Required: true},
		Record{Purpose: "DMARC: policy for failures", Type: "TXT", Name: "_dmarc." + domain, Value: "v=DMARC1; p=none; rua=mailto:postmaster@" + domain, Where: "dns", Required: true},
	)
	if m.cfg.ServerIPv4 != "" {
		recs = append(recs, Record{Purpose: "Reverse DNS (PTR): set at your hosting provider", Type: "PTR", Name: m.cfg.ServerIPv4, Value: h, Where: "host provider", Required: true})
	}
	return recs, nil
}

func norm(s string) string { return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), ".")) }

// Verify looks every record up and sets Status/Found.
func (m *Manager) Verify(ctx context.Context, domain string, r Resolver) ([]Record, error) {
	recs, err := m.Checklist(domain)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for i := range recs {
		rc := &recs[i]
		switch rc.Type {
		case "MX":
			mx, _ := r.LookupMX(ctx, domain)
			var got []string
			for _, x := range mx {
				got = append(got, fmt.Sprintf("%d %s", x.Pref, norm(x.Host)))
			}
			rc.Found = strings.Join(got, ", ")
			rc.Status = classify(len(got) > 0, containsHost(mx, m.cfg.Hostname))
		case "A", "AAAA":
			hosts, _ := r.LookupHost(ctx, rc.Name)
			rc.Found = strings.Join(hosts, ", ")
			rc.Status = classify(len(hosts) > 0, contains(hosts, rc.Value))
		case "PTR":
			names, _ := r.LookupAddr(ctx, rc.Name)
			var got []string
			for _, n := range names {
				got = append(got, norm(n))
			}
			rc.Found = strings.Join(got, ", ")
			rc.Status = classify(len(got) > 0, contains(got, norm(rc.Value)))
		case "TXT":
			txt, _ := r.LookupTXT(ctx, rc.Name)
			rc.Found = strings.Join(txt, " | ")
			rc.Status = txtStatus(rc, txt, m.cfg.ServerIPv4)
		}
	}
	return recs, nil
}

func classify(present, good bool) string {
	switch {
	case !present:
		return "missing"
	case good:
		return "ok"
	}
	return "wrong"
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

func containsHost(mx []*net.MX, host string) bool {
	for _, x := range mx {
		if norm(x.Host) == norm(host) {
			return true
		}
	}
	return false
}

func txtStatus(rc *Record, txt []string, ip string) string {
	switch {
	case strings.HasPrefix(rc.Value, "v=spf1"):
		for _, t := range txt {
			if strings.HasPrefix(t, "v=spf1") {
				ok := strings.Contains(t, " mx") || (ip != "" && strings.Contains(t, "ip4:"+ip))
				return classify(true, ok)
			}
		}
		return "missing"
	case strings.HasPrefix(rc.Value, "v=DKIM1"):
		// resolvers may return the key split in chunks; compare without spaces/quotes
		want := compact(rc.Value[strings.Index(rc.Value, "p=")+2:])
		for _, t := range txt {
			if strings.Contains(compact(t), "p="+want) || strings.Contains(compact(t), want) {
				return "ok"
			}
		}
		return classify(len(txt) > 0, false)
	case strings.HasPrefix(rc.Value, "v=DMARC1"):
		for _, t := range txt {
			if strings.HasPrefix(t, "v=DMARC1") {
				return "ok"
			}
		}
		return "missing"
	}
	return "missing"
}

func compact(s string) string { return strings.NewReplacer(" ", "", "\"", "", "\n", "").Replace(s) }

// SystemResolver uses the host's DNS.
type SystemResolver struct{}

func (SystemResolver) LookupMX(ctx context.Context, n string) ([]*net.MX, error) {
	return net.DefaultResolver.LookupMX(ctx, n)
}
func (SystemResolver) LookupTXT(ctx context.Context, n string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, n)
}
func (SystemResolver) LookupHost(ctx context.Context, n string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, n)
}
func (SystemResolver) LookupAddr(ctx context.Context, n string) ([]string, error) {
	return net.DefaultResolver.LookupAddr(ctx, n)
}
