package notifier

import "syscall"

func diskUsed() (float64, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs("/", &s); err != nil {
		return 0, err
	}
	total := float64(s.Blocks) * float64(s.Bsize)
	free := float64(s.Bavail) * float64(s.Bsize)
	if total == 0 {
		return 0, nil
	}
	return (total - free) / total * 100, nil
}
