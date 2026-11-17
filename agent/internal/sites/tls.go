package sites

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// IssueTLS requests a Let's Encrypt certificate over HTTP-01 and switches the Site to HTTPS.
// It runs as a Task because the CA validation can take a while and may fail on DNS problems.
func (s *Service) IssueTLS(domain string, withWWW bool, email string) (store.Task, error) {
	x, err := s.st.Get(domain)
	if err != nil {
		return store.Task{}, err
	}
	if email != "" && !emailRe.MatchString(email) {
		return store.Task{}, fmt.Errorf("%w: invalid email", ErrInvalid)
	}
	return s.tasks.Start("tls.issue", "Issue certificate for "+x.Domain, func(l *tasks.Log) error {
		if err := os.MkdirAll(filepath.Join(s.p.Root, "/var/www/letsencrypt"), 0o755); err != nil {
			return err
		}
		// Make sure the challenge location is served before asking the CA.
		s.mu.Lock()
		cur, err := s.st.Get(domain)
		if err == nil {
			_, err = s.commit(&cur, cur)
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
		args := []string{"certonly", "--webroot", "-w", "/var/www/letsencrypt", "-d", x.Domain,
			"--non-interactive", "--agree-tos", "--cert-name", x.Domain, "--deploy-hook", "systemctl reload nginx"}
		mode := "apex"
		if withWWW {
			args = append(args, "-d", "www."+x.Domain)
			mode = "www"
		}
		if email != "" {
			args = append(args, "--email", email)
		} else {
			args = append(args, "--register-unsafely-without-email")
		}
		if err := s.run.RunW(l, "certbot", args...); err != nil {
			return fmt.Errorf("certificate was not issued (check that the domain points to this server): %w", err)
		}
		return s.setTLS(domain, mode)
	})
}

// RemoveTLS switches the Site back to plain HTTP. The certificate stays on disk so it can be re-enabled.
func (s *Service) RemoveTLS(domain string) (store.Site, error) {
	return s.setTLSLocked(domain, "")
}

func (s *Service) setTLS(domain, mode string) error {
	_, err := s.setTLSLocked(domain, mode)
	return err
}

func (s *Service) setTLSLocked(domain, mode string) (store.Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.st.Get(domain)
	if err != nil {
		return old, err
	}
	x := old
	x.TLS = mode
	return s.commit(&old, x)
}
