package sites

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

var (
	packageRe    = regexp.MustCompile(`^[a-z0-9]([_.-]?[a-z0-9]+)*/[a-z0-9](([_.]|-{1,2})?[a-z0-9]+)*(:[A-Za-z0-9.*^~<>=|@-]{1,40})?$`)
	composerBins = []string{"/usr/bin/composer", "/usr/local/bin/composer"}
)

// ComposerInput is one Composer invocation. Only fixed sub-commands are accepted; nothing is passed to a shell.
type ComposerInput struct {
	Command string `json:"command"` // install | update | require | remove | dump-autoload | validate
	Package string `json:"package"` // for require/remove, optionally also for update
	Dir     string `json:"dir"`     // project folder relative to the Site home ("public_html" by default)
	NoDev   bool   `json:"no_dev"`  // install/update without require-dev packages
}

// ComposerState tells the UI whether Composer may run and where composer.json files are.
type ComposerState struct {
	Enabled   bool     `json:"enabled"`
	Installed bool     `json:"installed"`
	Projects  []string `json:"projects"`
}

func (s *Service) composerBin() string {
	for _, b := range composerBins {
		if _, err := os.Stat(b); err == nil {
			return b
		}
	}
	if s.p.Root != "" { // development run against a scratch root
		return composerBins[0]
	}
	return ""
}

func (s *Service) homeOf(x store.Site) string { return filepath.Join(s.p.SitesDir, x.User) }

// cleanProjectDir returns a relative folder inside the Site home ("public_html" when empty).
func cleanProjectDir(d string) (string, error) {
	d = strings.Trim(strings.TrimSpace(d), "/")
	if d == "" {
		return "public_html", nil
	}
	for _, part := range strings.Split(d, "/") {
		if part == "." || part == ".." || part == "" {
			return "", fmt.Errorf("%w: folder", ErrInvalid)
		}
	}
	if !docrootRe.MatchString(d) || strings.HasPrefix(d, ".trash") {
		return "", fmt.Errorf("%w: folder", ErrInvalid)
	}
	return path.Clean(d), nil
}

// Composer reports whether Composer is allowed for the Site and lists the projects found.
func (s *Service) Composer(domain string) (ComposerState, error) {
	x, err := s.st.Get(domain)
	if err != nil {
		return ComposerState{}, err
	}
	st := ComposerState{Enabled: x.Composer, Installed: s.composerBin() != "", Projects: []string{}}
	home := s.homeOf(x)
	_ = filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(home, p)
		if d.IsDir() {
			n := d.Name()
			if n == "vendor" || n == "node_modules" || n == ".trash" || n == ".composer" || n == ".git" || strings.Count(rel, "/") >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "composer.json" {
			dir := filepath.Dir(rel)
			if dir == "." {
				dir = ""
			}
			st.Projects = append(st.Projects, dir)
		}
		return nil
	})
	sort.Strings(st.Projects)
	return st, nil
}

// RunComposer runs Composer as the Site User in a Task. It refuses unless the Administrator enabled Composer for the Site.
func (s *Service) RunComposer(domain string, in ComposerInput) (store.Task, error) {
	x, err := s.st.Get(domain)
	if err != nil {
		return store.Task{}, err
	}
	if !x.Composer {
		return store.Task{}, fmt.Errorf("%w: Composer is not enabled for this site", ErrInvalid)
	}
	if x.Status != "enabled" {
		return store.Task{}, fmt.Errorf("%w: the site is disabled", ErrInvalid)
	}
	bin := s.composerBin()
	if bin == "" {
		return store.Task{}, fmt.Errorf("%w: Composer is not installed on the server; install it in Software", ErrInvalid)
	}
	if s.RuntimeInstalled != nil && !s.RuntimeInstalled(x.PHPVersion) {
		return store.Task{}, fmt.Errorf("%w: PHP %s is not installed", ErrInvalid, x.PHPVersion)
	}
	dir, err := cleanProjectDir(in.Dir)
	if err != nil {
		return store.Task{}, err
	}
	home := s.homeOf(x)
	args := []string{}
	needJSON := true
	switch in.Command {
	case "install":
		args = []string{"install", "--prefer-dist", "--no-progress"}
		if in.NoDev {
			args = append(args, "--no-dev")
		}
	case "update":
		args = []string{"update", "--prefer-dist", "--no-progress"}
		if in.NoDev {
			args = append(args, "--no-dev")
		}
		if in.Package != "" {
			if !packageRe.MatchString(in.Package) {
				return store.Task{}, fmt.Errorf("%w: package name", ErrInvalid)
			}
			args = append(args, in.Package)
		}
	case "require", "remove":
		if !packageRe.MatchString(in.Package) {
			return store.Task{}, fmt.Errorf("%w: package name, expected vendor/name or vendor/name:^1.0", ErrInvalid)
		}
		args = []string{in.Command, "--prefer-dist", "--no-progress", in.Package}
		needJSON = in.Command == "remove"
	case "dump-autoload":
		args = []string{"dump-autoload", "--optimize"}
	case "validate":
		args = []string{"validate", "--no-check-publish"}
	default:
		return store.Task{}, fmt.Errorf("%w: command", ErrInvalid)
	}
	if st, err := os.Stat(filepath.Join(home, dir)); err != nil || !st.IsDir() {
		return store.Task{}, fmt.Errorf("%w: folder %q does not exist", ErrInvalid, dir)
	}
	if needJSON {
		if _, err := os.Stat(filepath.Join(home, dir, "composer.json")); err != nil {
			return store.Task{}, fmt.Errorf("%w: no composer.json in %s", ErrInvalid, dir)
		}
	}
	title := "Composer " + in.Command
	if in.Package != "" {
		title += " " + in.Package
	}
	title += " — " + domain
	full := append([]string{"-u", x.User, "--", "php" + x.PHPVersion, bin}, append(args, "--no-interaction", "--no-ansi")...)
	env := []string{"HOME=" + home, "COMPOSER_HOME=" + filepath.Join(home, ".composer"), "COMPOSER_CACHE_DIR=" + filepath.Join(home, ".composer", "cache"),
		"COMPOSER_NO_INTERACTION=1", "COMPOSER_DISABLE_XDEBUG_WARN=1"}
	return s.tasks.Start("composer", title, func(l *tasks.Log) error {
		l.Printf("$ composer %s  (in ~/%s, as %s, PHP %s)", strings.Join(args, " "), dir, x.User, x.PHPVersion)
		return s.run.RunEnvW(l, env, filepath.Join(home, dir), "runuser", full...)
	})
}
