package sysmgr

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

type Container struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	State  string `json:"state"`
	Status string `json:"status"`
	Ports  string `json:"ports"`
}

type Image struct {
	ID      string `json:"id"`
	Repo    string `json:"repo"`
	Tag     string `json:"tag"`
	Size    string `json:"size"`
	Created string `json:"created"`
}

type Docker struct {
	Installed  bool        `json:"installed"`
	Running    bool        `json:"running"`
	Version    string      `json:"version"`
	Containers []Container `json:"containers"`
	Images     []Image     `json:"images"`
}

func jsonLines(out string) []map[string]string {
	res := []map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		var r map[string]string
		if json.Unmarshal([]byte(l), &r) == nil {
			res = append(res, r)
		}
	}
	return res
}

func (m *Manager) Docker() Docker {
	d := Docker{Containers: []Container{}, Images: []Image{}}
	ds := m.dpkgState([]string{"docker.io"})
	if len(ds) == 0 {
		return d
	}
	d.Installed = true
	d.Version = ds["docker.io"]
	if !m.unitStates([]string{"docker.service"})["docker.service"].Active {
		return d
	}
	d.Running = true
	for _, r := range jsonLines(m.out("docker", "ps", "-a", "--format", "{{json .}}")) {
		d.Containers = append(d.Containers, Container{ID: r["ID"], Name: r["Names"], Image: r["Image"], State: r["State"], Status: r["Status"], Ports: r["Ports"]})
	}
	for _, r := range jsonLines(m.out("docker", "images", "--format", "{{json .}}")) {
		d.Images = append(d.Images, Image{ID: r["ID"], Repo: r["Repository"], Tag: r["Tag"], Size: r["Size"], Created: r["CreatedSince"]})
	}
	return d
}

var (
	ctrIDRe  = regexp.MustCompile(`^[a-f0-9]{6,64}$`)
	imageRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,120}(:[A-Za-z0-9._-]{1,80})?(@sha256:[a-f0-9]{64})?$`)
	envRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,60}=[^\r\n\x00]*$`)
	mapPortR = regexp.MustCompile(`^(\d{1,3}(\.\d{1,3}){3}:)?\d{1,5}:\d{1,5}(/(tcp|udp))?$`)
)

var ctrActions = map[string]bool{"start": true, "stop": true, "restart": true, "kill": true, "pause": true, "unpause": true}

// ContainerAction runs an action on a container, or removes it ("remove").
func (m *Manager) ContainerAction(id, action string) error {
	if !ctrIDRe.MatchString(id) {
		return invalid("container id")
	}
	if action == "remove" {
		return m.Run.Run("docker", "rm", "-f", id)
	}
	if !ctrActions[action] {
		return invalid("action")
	}
	return m.Run.Run("docker", action, id)
}

func (m *Manager) ContainerLog(id string, lines int) (string, error) {
	if !ctrIDRe.MatchString(id) {
		return "", invalid("container id")
	}
	if lines <= 0 || lines > 2000 {
		lines = 200
	}
	out, err := m.Run.Output("docker", "logs", "--tail", fmt.Sprint(lines), "--timestamps", id)
	if err != nil && out == "" {
		return "", err
	}
	return out, nil
}

type RunInput struct {
	Image   string   `json:"image"`
	Name    string   `json:"name"`
	Ports   []string `json:"ports"`
	Env     []string `json:"env"`
	Volumes []string `json:"volumes"`
	Restart string   `json:"restart"` // no | unless-stopped | always | on-failure
	Memory  string   `json:"memory"`  // e.g. 512m
}

var memRe = regexp.MustCompile(`^[1-9][0-9]{0,5}[mg]$`)

// RunContainer pulls (if needed) and starts a container as a Task.
func (m *Manager) RunContainer(in RunInput) (store.Task, error) {
	if !imageRe.MatchString(in.Image) {
		return store.Task{}, invalid("image name")
	}
	if in.Name != "" && !nameRe.MatchString(in.Name) {
		return store.Task{}, invalid("container name")
	}
	args := []string{"run", "-d"}
	if in.Name != "" {
		args = append(args, "--name", in.Name)
	}
	switch in.Restart {
	case "", "no":
	case "unless-stopped", "always", "on-failure":
		args = append(args, "--restart", in.Restart)
	default:
		return store.Task{}, invalid("restart policy")
	}
	if in.Memory != "" {
		if !memRe.MatchString(in.Memory) {
			return store.Task{}, invalid("memory limit like 512m or 2g")
		}
		args = append(args, "--memory", in.Memory)
	}
	for _, p := range in.Ports {
		if !mapPortR.MatchString(p) {
			return store.Task{}, invalid("port mapping %q (use host:container, e.g. 8080:80)", p)
		}
		args = append(args, "-p", p)
	}
	for _, e := range in.Env {
		if len(e) > 2000 || !envRe.MatchString(e) {
			return store.Task{}, invalid("environment variable %q (use NAME=value)", e)
		}
		args = append(args, "-e", e)
	}
	for _, v := range in.Volumes {
		host, rest, ok := strings.Cut(v, ":")
		if !ok || !filepath.IsAbs(host) || strings.Contains(host, "..") || !strings.HasPrefix(rest, "/") || strings.ContainsAny(v, "\r\n\x00") {
			return store.Task{}, invalid("volume %q (use /host/path:/container/path)", v)
		}
		for _, bad := range []string{"/", "/etc", "/root", "/var/run/docker.sock", "/run/docker.sock", "/proc", "/sys", "/boot"} {
			if filepath.Clean(host) == bad {
				return store.Task{}, invalid("mounting %s into a container is not allowed", bad)
			}
		}
		args = append(args, "-v", v)
	}
	args = append(args, in.Image)
	return m.Tasks.Start("docker.run", "Run "+in.Image, func(l *tasks.Log) error {
		return m.Run.RunW(l, "docker", args...)
	})
}

func (m *Manager) PullImage(image string) (store.Task, error) {
	if !imageRe.MatchString(image) {
		return store.Task{}, invalid("image name")
	}
	return m.Tasks.Start("docker.pull", "Pull "+image, func(l *tasks.Log) error {
		return m.Run.RunW(l, "docker", "pull", image)
	})
}

func (m *Manager) RemoveImage(id string) error {
	if !regexp.MustCompile(`^[a-f0-9]{6,64}$`).MatchString(id) {
		return invalid("image id")
	}
	return m.Run.Run("docker", "rmi", id)
}
