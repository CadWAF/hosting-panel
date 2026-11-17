package sites

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"hostingpanel/agent/internal/store"
)

var (
	proxyPathRe  = regexp.MustCompile(`^/[A-Za-z0-9._~/-]{0,200}$`)
	upstreamRe   = regexp.MustCompile(`^https?://([A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?|\[[0-9a-fA-F:]{2,39}\])(:[0-9]{1,5})?(/[A-Za-z0-9._~/-]{0,200})?$`)
	redirectToRe = regexp.MustCompile(`^(https?://[^\s"'\;{}$` + "`" + `]{1,500}|/[^\s"'\;{}$` + "`" + `]{0,500})$`)
	authUserRe   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
)

const (
	maxProxies   = 20
	maxRedirects = 50
)

// WebInput are the optional changes to a Site's web settings; nil leaves a field as it is.
type WebInput struct {
	MaxBodyMB    *int              `json:"max_body_mb"`
	Gzip         *bool             `json:"gzip"`
	StaticCache  *bool             `json:"static_cache"`
	Redirects    *[]store.Redirect `json:"redirects"`
	Proxies      *[]store.Proxy    `json:"proxies"`
	AuthUser     *string           `json:"auth_user"`     // "" turns basic auth off
	AuthPassword string            `json:"auth_password"` // set together with a user to (re)write the password file
}

// applyWeb validates in on top of cur. It returns the new config and, when a password was supplied, its bcrypt hash.
func applyWeb(cur store.WebConfig, in WebInput) (store.WebConfig, string, error) {
	w := cur
	if in.MaxBodyMB != nil {
		if *in.MaxBodyMB < 0 || *in.MaxBodyMB > 10240 {
			return w, "", fmt.Errorf("%w: upload size must be between 1 and 10240 MB", ErrInvalid)
		}
		w.MaxBodyMB = *in.MaxBodyMB
	}
	if in.Gzip != nil {
		w.Gzip = *in.Gzip
	}
	if in.StaticCache != nil {
		w.StaticCache = *in.StaticCache
	}
	if in.Redirects != nil {
		if len(*in.Redirects) > maxRedirects {
			return w, "", fmt.Errorf("%w: at most %d redirects", ErrInvalid, maxRedirects)
		}
		seen := map[string]bool{}
		out := []store.Redirect{}
		for _, r := range *in.Redirects {
			if r.Code == 0 {
				r.Code = 301
			}
			if !proxyPathRe.MatchString(r.From) || r.From == "/" || strings.Contains(r.From, "..") {
				return w, "", fmt.Errorf("%w: redirect source must be a path such as /old-page", ErrInvalid)
			}
			if !redirectToRe.MatchString(r.To) {
				return w, "", fmt.Errorf("%w: redirect target must be a URL or a path", ErrInvalid)
			}
			if r.Code != 301 && r.Code != 302 {
				return w, "", fmt.Errorf("%w: redirect code must be 301 or 302", ErrInvalid)
			}
			if seen[r.From] {
				return w, "", fmt.Errorf("%w: duplicate redirect %s", ErrInvalid, r.From)
			}
			seen[r.From] = true
			out = append(out, r)
		}
		w.Redirects = out
	}
	if in.Proxies != nil {
		if len(*in.Proxies) > maxProxies {
			return w, "", fmt.Errorf("%w: at most %d proxies", ErrInvalid, maxProxies)
		}
		seen := map[string]bool{}
		out := []store.Proxy{}
		for _, p := range *in.Proxies {
			if !proxyPathRe.MatchString(p.Path) || strings.Contains(p.Path, "..") {
				return w, "", fmt.Errorf("%w: proxy path must look like / or /app", ErrInvalid)
			}
			if !upstreamRe.MatchString(p.Upstream) {
				return w, "", fmt.Errorf("%w: upstream must look like http://127.0.0.1:8080", ErrInvalid)
			}
			if seen[p.Path] {
				return w, "", fmt.Errorf("%w: duplicate proxy path %s", ErrInvalid, p.Path)
			}
			seen[p.Path] = true
			out = append(out, p)
		}
		w.Proxies = out
	}
	hash := ""
	if in.AuthUser != nil {
		u := strings.TrimSpace(*in.AuthUser)
		if u != "" && !authUserRe.MatchString(u) {
			return w, "", fmt.Errorf("%w: user name may use letters, digits . _ -", ErrInvalid)
		}
		w.AuthUser = u
	}
	if in.AuthPassword != "" {
		if w.AuthUser == "" {
			return w, "", fmt.Errorf("%w: set a user name for the password", ErrInvalid)
		}
		if len(in.AuthPassword) < 8 || len(in.AuthPassword) > 72 {
			return w, "", fmt.Errorf("%w: password must be 8–72 characters", ErrInvalid)
		}
		b, err := bcrypt.GenerateFromPassword([]byte(in.AuthPassword), 10)
		if err != nil {
			return w, "", err
		}
		hash = string(b)
	}
	return w, hash, nil
}

func (s *Service) authFile(domain string) string {
	return filepath.Join(s.p.Root, "/etc/nginx/hostingpanel-auth", domain+".htpasswd")
}

// writeAuthFile stores user:hash for nginx (root-owned, readable by the nginx master that reads it as root).
func (s *Service) writeAuthFile(domain, user, hash string) error {
	p := s.authFile(domain)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(user+":"+hash+"\n"), 0o640)
}

func (s *Service) authFileHasHash(domain string) bool {
	b, err := os.ReadFile(s.authFile(domain))
	return err == nil && strings.Contains(string(b), ":")
}

// webDirectives renders the nginx directives and locations for a Site's web settings.
// apachePort > 0 means static-cache requests are proxied to Apache instead of read from disk.
func (s *Service) webDirectives(x store.Site, apachePort int) (pre string, rootProxy bool, maxBody string) {
	w := x.Web
	mb := w.MaxBodyMB
	if mb == 0 {
		mb = 128
	}
	maxBody = fmt.Sprintf("%dm", mb)
	var b strings.Builder
	if w.Gzip {
		b.WriteString("    gzip on;\n    gzip_vary on;\n    gzip_min_length 1024;\n    gzip_types text/plain text/css text/xml application/json application/javascript application/xml image/svg+xml;\n")
	}
	if w.AuthUser != "" && s.authFileHasHash(x.Domain) {
		fmt.Fprintf(&b, "    auth_basic \"Restricted\";\n    auth_basic_user_file %s;\n", s.authFile(x.Domain))
	}
	for _, r := range w.Redirects {
		if strings.HasSuffix(r.From, "/") {
			fmt.Fprintf(&b, "    location ^~ %s {\n        return %d %s;\n    }\n", r.From, r.Code, r.To)
		} else {
			fmt.Fprintf(&b, "    location = %s {\n        return %d %s;\n    }\n", r.From, r.Code, r.To)
		}
	}
	for _, p := range w.Proxies {
		mod := "^~ "
		path := p.Path
		if path == "/" {
			mod, rootProxy = "", true
		}
		fmt.Fprintf(&b, "    location %s%s {\n        proxy_pass %s;\n        proxy_http_version 1.1;\n        proxy_set_header Host $host;\n        proxy_set_header X-Real-IP $remote_addr;\n        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n        proxy_set_header X-Forwarded-Proto $scheme;\n        proxy_read_timeout 300s;\n", mod, path, p.Upstream)
		if p.WebSocket {
			b.WriteString("        proxy_set_header Upgrade $http_upgrade;\n        proxy_set_header Connection $connection_upgrade;\n")
		}
		b.WriteString("    }\n")
	}
	if w.StaticCache && !rootProxy {
		b.WriteString("    location ~* \\.(?:css|js|mjs|jpe?g|png|gif|webp|avif|svg|ico|woff2?|ttf|eot|mp4|webm)$ {\n        expires 30d;\n        add_header Cache-Control \"public\";\n        access_log off;\n")
		if apachePort > 0 {
			fmt.Fprintf(&b, "        proxy_pass http://127.0.0.1:%d;\n        proxy_set_header Host $host;\n", apachePort)
		} else {
			b.WriteString("        try_files $uri =404;\n")
		}
		b.WriteString("    }\n")
	}
	return b.String(), rootProxy, maxBody
}

// needsUpgradeMap reports whether any proxy passes WebSocket headers (the http-level map must then exist).
func needsUpgradeMap(x store.Site) bool {
	for _, p := range x.Web.Proxies {
		if p.WebSocket {
			return true
		}
	}
	return false
}
