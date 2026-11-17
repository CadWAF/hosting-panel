package sysmgr

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Point is one monitoring sample.
type Point struct {
	T      int64   `json:"t"`       // unix seconds
	CPU    float64 `json:"cpu"`     // percent of all cores
	Mem    float64 `json:"mem"`     // percent used
	Load1  float64 `json:"load1"`   // 1-minute load average
	NetRx  float64 `json:"net_rx"`  // bytes per second
	NetTx  float64 `json:"net_tx"`  // bytes per second
	DiskRd float64 `json:"disk_rd"` // bytes per second
	DiskWr float64 `json:"disk_wr"` // bytes per second
}

type Disk struct {
	Mount string `json:"mount"`
	FS    string `json:"fs"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

type Proc struct {
	PID  int     `json:"pid"`
	Name string  `json:"name"`
	User string  `json:"user"`
	CPU  float64 `json:"cpu"`
	Mem  uint64  `json:"mem"` // resident bytes
}

type Stats struct {
	Hostname  string     `json:"hostname"`
	OS        string     `json:"os"`
	Kernel    string     `json:"kernel"`
	Cores     int        `json:"cores"`
	Uptime    int64      `json:"uptime"`
	Load      [3]float64 `json:"load"`
	Now       Point      `json:"now"`
	MemTotal  uint64     `json:"mem_total"`
	MemUsed   uint64     `json:"mem_used"`
	SwapTotal uint64     `json:"swap_total"`
	SwapUsed  uint64     `json:"swap_used"`
	Disks     []Disk     `json:"disks"`
	Procs     []Proc     `json:"procs"`
	History   []Point    `json:"history"`
}

type cpuTimes struct{ idle, total uint64 }

type sampler struct {
	mu       sync.Mutex
	ring     []Point
	lastCPU  cpuTimes
	lastNet  [2]uint64
	lastDisk [2]uint64
	lastAt   time.Time
	procPrev map[int]uint64
	procAt   time.Time
}

const ringSize = 720 // one hour at 5 s

func newSampler() *sampler { return &sampler{procPrev: map[int]uint64{}} }

func readFile(p string) string { b, _ := os.ReadFile(p); return string(b) }

func readCPU() cpuTimes {
	for _, l := range strings.Split(readFile("/proc/stat"), "\n") {
		f := strings.Fields(l)
		if len(f) > 5 && f[0] == "cpu" {
			var t, idle uint64
			for i, v := range f[1:] {
				n, _ := strconv.ParseUint(v, 10, 64)
				if i < 8 { // user nice system idle iowait irq softirq steal
					t += n
				}
				if i == 3 || i == 4 {
					idle += n
				}
			}
			return cpuTimes{idle: idle, total: t}
		}
	}
	return cpuTimes{}
}

func readNet() (rx, tx uint64) {
	lines := strings.Split(readFile("/proc/net/dev"), "\n")
	if len(lines) < 3 {
		return
	}
	for _, l := range lines[2:] {
		name, rest, ok := strings.Cut(l, ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		f := strings.Fields(rest)
		if len(f) >= 9 {
			a, _ := strconv.ParseUint(f[0], 10, 64)
			b, _ := strconv.ParseUint(f[8], 10, 64)
			rx += a
			tx += b
		}
	}
	return
}

// readDisk sums sectors read/written over whole physical disks (not partitions).
func readDisk() (rd, wr uint64) {
	for _, l := range strings.Split(readFile("/proc/diskstats"), "\n") {
		f := strings.Fields(l)
		if len(f) < 10 {
			continue
		}
		n := f[2]
		if strings.HasPrefix(n, "loop") || strings.HasPrefix(n, "ram") || strings.HasPrefix(n, "dm-") {
			continue
		}
		if _, err := os.Stat("/sys/block/" + n); err != nil {
			continue // a partition
		}
		a, _ := strconv.ParseUint(f[5], 10, 64)
		b, _ := strconv.ParseUint(f[9], 10, 64)
		rd += a * 512
		wr += b * 512
	}
	return
}

func memInfo() (total, used, swapT, swapU uint64) {
	m := map[string]uint64{}
	for _, l := range strings.Split(readFile("/proc/meminfo"), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) > 0 {
			n, _ := strconv.ParseUint(f[0], 10, 64)
			m[k] = n * 1024
		}
	}
	total = m["MemTotal"]
	avail := m["MemAvailable"]
	if avail > total {
		avail = total
	}
	return total, total - avail, m["SwapTotal"], m["SwapTotal"] - m["SwapFree"]
}

func loadAvg() (l [3]float64) {
	f := strings.Fields(readFile("/proc/loadavg"))
	for i := 0; i < 3 && i < len(f); i++ {
		l[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return
}

// Sample takes one measurement and appends it to the history.
func (m *Manager) Sample() Point {
	s := m.stats
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	c := readCPU()
	rx, tx := readNet()
	rd, wr := readDisk()
	total, used, _, _ := memInfo()
	p := Point{T: now.Unix(), Load1: loadAvg()[0]}
	if total > 0 {
		p.Mem = float64(used) / float64(total) * 100
	}
	if !s.lastAt.IsZero() {
		dt := now.Sub(s.lastAt).Seconds()
		if dt > 0 {
			if d := c.total - s.lastCPU.total; d > 0 && c.total >= s.lastCPU.total {
				p.CPU = (1 - float64(c.idle-s.lastCPU.idle)/float64(d)) * 100
			}
			p.NetRx = rate(rx, s.lastNet[0], dt)
			p.NetTx = rate(tx, s.lastNet[1], dt)
			p.DiskRd = rate(rd, s.lastDisk[0], dt)
			p.DiskWr = rate(wr, s.lastDisk[1], dt)
		}
	}
	s.lastCPU, s.lastNet, s.lastDisk, s.lastAt = c, [2]uint64{rx, tx}, [2]uint64{rd, wr}, now
	s.ring = append(s.ring, p)
	if len(s.ring) > ringSize {
		s.ring = s.ring[len(s.ring)-ringSize:]
	}
	return p
}

func rate(cur, prev uint64, dt float64) float64 {
	if cur < prev {
		return 0
	}
	return float64(cur-prev) / dt
}

// RunSampler samples every 5 seconds until stop is closed.
func (m *Manager) RunSampler(stop <-chan struct{}) {
	m.Sample()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.Sample()
		}
	}
}

func osRelease() string {
	for _, l := range strings.Split(readFile("/etc/os-release"), "\n") {
		if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

func disks() []Disk {
	seen := map[string]bool{}
	out := []Disk{}
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) < 3 || !strings.HasPrefix(p[0], "/dev/") || strings.HasPrefix(p[0], "/dev/loop") || seen[p[0]] || strings.HasPrefix(p[1], "/etc/") || strings.HasPrefix(p[1], "/var/lib/docker/") {
			continue // device already listed, or a container bind mount
		}
		var st syscall.Statfs_t
		if syscall.Statfs(p[1], &st) != nil {
			continue
		}
		seen[p[0]] = true
		total := uint64(st.Blocks) * uint64(st.Bsize)
		free := uint64(st.Bfree) * uint64(st.Bsize)
		out = append(out, Disk{Mount: p[1], FS: p[2], Total: total, Used: total - free})
	}
	return out
}

func (m *Manager) topProcs(n int) []Proc {
	s := m.stats
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	dt := now.Sub(s.procAt).Seconds()
	cur := map[int]uint64{}
	ents, _ := os.ReadDir("/proc")
	procs := []Proc{}
	hz := 100.0
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat := readFile("/proc/" + e.Name() + "/stat")
		i, j := strings.Index(stat, "("), strings.LastIndex(stat, ")")
		if i < 0 || j < i {
			continue
		}
		rest := strings.Fields(stat[j+1:])
		if len(rest) < 22 {
			continue
		}
		ut, _ := strconv.ParseUint(rest[11], 10, 64)
		st, _ := strconv.ParseUint(rest[12], 10, 64)
		rss, _ := strconv.ParseUint(rest[21], 10, 64)
		ticks := ut + st
		cur[pid] = ticks
		p := Proc{PID: pid, Name: stat[i+1 : j], Mem: rss * uint64(os.Getpagesize()), User: procUser(pid)}
		if prev, ok := s.procPrev[pid]; ok && dt > 0 && ticks >= prev {
			p.CPU = float64(ticks-prev) / hz / dt * 100
		}
		procs = append(procs, p)
	}
	s.procPrev, s.procAt = cur, now
	sort.Slice(procs, func(a, b int) bool {
		if procs[a].CPU != procs[b].CPU {
			return procs[a].CPU > procs[b].CPU
		}
		return procs[a].Mem > procs[b].Mem
	})
	if len(procs) > n {
		procs = procs[:n]
	}
	return procs
}

func procUser(pid int) string {
	for _, l := range strings.Split(readFile("/proc/"+strconv.Itoa(pid)+"/status"), "\n") {
		if v, ok := strings.CutPrefix(l, "Uid:"); ok {
			f := strings.Fields(v)
			if len(f) > 0 {
				return uidName(f[0])
			}
		}
	}
	return ""
}

var uidCache sync.Map

func uidName(uid string) string {
	if v, ok := uidCache.Load(uid); ok {
		return v.(string)
	}
	name := uid
	for _, l := range strings.Split(readFile("/etc/passwd"), "\n") {
		f := strings.Split(l, ":")
		if len(f) > 2 && f[2] == uid {
			name = f[0]
			break
		}
	}
	uidCache.Store(uid, name)
	return name
}

// Stats returns a full snapshot, including the recent history for charts.
func (m *Manager) Stats() Stats {
	host, _ := os.Hostname()
	up := atof(strings.Fields(readFile("/proc/uptime") + " 0")[0])
	total, used, swT, swU := memInfo()
	m.stats.mu.Lock()
	hist := append([]Point(nil), m.stats.ring...)
	m.stats.mu.Unlock()
	var now Point
	if len(hist) > 0 {
		now = hist[len(hist)-1]
	}
	cores := 0
	for _, l := range strings.Split(readFile("/proc/cpuinfo"), "\n") {
		if strings.HasPrefix(l, "processor") {
			cores++
		}
	}
	return Stats{
		Hostname: host, OS: osRelease(), Kernel: strings.TrimSpace(readFile("/proc/sys/kernel/osrelease")),
		Cores: cores, Uptime: int64(up), Load: loadAvg(), Now: now,
		MemTotal: total, MemUsed: used, SwapTotal: swT, SwapUsed: swU,
		Disks: disks(), Procs: m.topProcs(8), History: hist,
	}
}
